package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

// ─── IP helpers ──────────────────────────────────────────────

// stripPort removes :port if present (handles IPv4:port and [IPv6]:port).
func stripPort(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.Contains(s, "]") {
		if h, _, err := net.SplitHostPort(s); err == nil {
			return strings.Trim(h, "[]")
		}
		s = strings.Trim(s, "[]")
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}

// normalize strips :port, zone ids, and ::ffff: prefix.
func normalize(ip string) string {
	ip = stripPort(ip)
	if i := strings.Index(ip, "%"); i != -1 { // zone id
		ip = ip[:i]
	}
	ip = strings.TrimPrefix(ip, "::ffff:")
	return strings.TrimSpace(ip)
}

// extractIPs pulls IPv4/IPv6 from request headers and remote addr.
func extractIPs(r *http.Request) (ipv4, ipv6, rawXFF, remote string) {
	rawXFF = r.Header.Get("X-Forwarded-For")

	// prefer CF-Connecting-IP if present (Cloudflare or header_up)
	if cf := r.Header.Get("CF-Connecting-IP"); cf != "" {
		if ip := net.ParseIP(normalize(cf)); ip != nil {
			if ip.To4() != nil {
				ipv4 = ip.String()
			} else {
				ipv6 = ip.String()
			}
		}
	}

	// parse X-Forwarded-For (may contain multiple, comma-separated)
	if rawXFF != "" && (ipv4 == "" || ipv6 == "") {
		for _, p := range strings.Split(rawXFF, ",") {
			ip := net.ParseIP(normalize(p))
			if ip == nil {
				continue
			}
			if ip.To4() != nil && ipv4 == "" {
				ipv4 = ip.String()
			}
			if ip.To4() == nil && ipv6 == "" {
				ipv6 = ip.String()
			}
			if ipv4 != "" && ipv6 != "" {
				break
			}
		}
	}

	// fallback to remote addr of the TCP peer (Caddy -> app)
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remote = normalize(host)
		if ipv4 == "" && ipv6 == "" {
			if ip := net.ParseIP(remote); ip != nil {
				if ip.To4() != nil {
					ipv4 = remote
				} else {
					ipv6 = remote
				}
			}
		}
	}
	return
}

// clientIP returns the best single IP (IPv6 preferred).
func clientIP(r *http.Request) string {
	ipv4, ipv6, _, _ := extractIPs(r)
	if ipv6 != "" {
		return ipv6
	}
	return ipv4
}

// ─── GeoIP cache ─────────────────────────────────────────────

// geoEntry holds cached geo lookup result.
type geoEntry struct {
	data      *GeoResponse
	fetchedAt time.Time
}

var (
	geoCache = make(map[string]*geoEntry)
	geoMu    sync.RWMutex
	geoTTL   = 5 * time.Minute
	// The geo provider is only reachable over plaintext HTTP on its free tier,
	// so an on-path attacker can answer in its place. Never follow a redirect
	// it hands us: that would let such an attacker point the lookup at a host
	// of their choosing.
	httpClient = &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
)

// GeoResponse — формат відповіді /json (сумісний з ipinfo.io).
type GeoResponse struct {
	IP       string `json:"ip"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Loc      string `json:"loc"`
	Org      string `json:"org"`
	Timezone string `json:"timezone"`
}

// ipAPIResponse — формат відповіді ip-api.com.
type ipAPIResponse struct {
	Status      string  `json:"status"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Region      string  `json:"region"`
	RegionName  string  `json:"regionName"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Timezone    string  `json:"timezone"`
	ISP         string  `json:"isp"`
	Org         string  `json:"org"`
	AS          string  `json:"as"`
	Query       string  `json:"query"`
}

const (
	// geoMaxBody caps how much of the untrusted geo reply we buffer.
	geoMaxBody = 64 << 10
	// geoMaxTextLen caps any single free-text field taken from the reply.
	geoMaxTextLen = 128
	// geoMaxTimezoneLen caps the timezone field taken from the reply.
	geoMaxTimezoneLen = 64
)

// safeGeoText reports whether a free-text field of the geo reply (city, region,
// org) is acceptable: bounded in length and printable, so that control
// characters or unbounded blobs are never cached and relayed to clients.
func safeGeoText(s string) bool {
	if len(s) > geoMaxTextLen {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// safeGeoCountryCode reports whether s is an ISO-3166-1 alpha-2 code (or empty,
// which the provider returns when it has no answer).
func safeGeoCountryCode(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
}

// safeGeoTimezone reports whether s looks like an IANA timezone name.
func safeGeoTimezone(s string) bool {
	if len(s) > geoMaxTimezoneLen {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '/', r == '_', r == '-', r == '+':
		default:
			return false
		}
	}
	return true
}

// lookupGeo fetches geo data from ip-api.com with in-memory caching.
func lookupGeo(ip string) (*GeoResponse, error) {
	// check cache
	geoMu.RLock()
	if entry, ok := geoCache[ip]; ok && time.Since(entry.fetchedAt) < geoTTL {
		geoMu.RUnlock()
		return entry.data, nil
	}
	geoMu.RUnlock()

	// fetch from ip-api.com (free tier: no key, plaintext HTTP only — the
	// provider serves TLS to paying customers only). The channel is therefore
	// neither confidential nor authenticated: anyone on the path can read the
	// queried IP and can forge the whole reply, so everything below treats the
	// response as hostile input before it is cached and served to clients.
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,countryCode,regionName,city,lat,lon,timezone,isp,org,as,query", ip)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("geo lookup failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geo lookup: http status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, geoMaxBody))
	if err != nil {
		return nil, fmt.Errorf("geo read body: %w", err)
	}

	var raw ipAPIResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("geo parse: %w", err)
	}

	if raw.Status != "success" {
		return nil, fmt.Errorf("geo lookup: status=%s", raw.Status)
	}

	// validate the untrusted reply before it is mapped, cached and returned
	if q := net.ParseIP(raw.Query); q == nil || !q.Equal(net.ParseIP(ip)) {
		return nil, fmt.Errorf("geo lookup: reply is for a different IP")
	}
	if !safeGeoText(raw.City) || !safeGeoText(raw.RegionName) || !safeGeoText(raw.AS) {
		return nil, fmt.Errorf("geo lookup: reply has unacceptable text fields")
	}
	if !safeGeoCountryCode(raw.CountryCode) {
		return nil, fmt.Errorf("geo lookup: reply has unacceptable country code")
	}
	if !safeGeoTimezone(raw.Timezone) {
		return nil, fmt.Errorf("geo lookup: reply has unacceptable timezone")
	}
	if raw.Lat < -90 || raw.Lat > 90 || raw.Lon < -180 || raw.Lon > 180 {
		return nil, fmt.Errorf("geo lookup: reply has out-of-range coordinates")
	}

	// map to ipinfo.io-compatible format
	geo := &GeoResponse{
		IP:       ip,
		City:     raw.City,
		Region:   raw.RegionName,
		Country:  raw.CountryCode,
		Loc:      fmt.Sprintf("%.4f,%.4f", raw.Lat, raw.Lon),
		Org:      raw.AS,
		Timezone: raw.Timezone,
	}

	// store in cache
	geoMu.Lock()
	geoCache[ip] = &geoEntry{data: geo, fetchedAt: time.Now()}
	geoMu.Unlock()

	return geo, nil
}

// ─── HTTP handlers ───────────────────────────────────────────

// writeJSON writes v as pretty-printed JSON (jq-style, 2-space indent).
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// jsonHandler returns IP details as JSON (existing /ip endpoint).
func jsonHandler(w http.ResponseWriter, r *http.Request) {
	ipv4, ipv6, xff, remote := extractIPs(r)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"ipv4":            ipv4,
		"ipv6":            ipv6,
		"x_forwarded_for": xff,
		"remote_addr":     remote,
		"ua":              r.UserAgent(),
	})
}

// textHandler returns the client IP as plain text (IPv6 preferred).
func textHandler(w http.ResponseWriter, r *http.Request) {
	ipv4, ipv6, _, _ := extractIPs(r)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if ipv6 != "" {
		w.Write([]byte(ipv6 + "\n"))
		return
	}
	if ipv4 != "" {
		w.Write([]byte(ipv4 + "\n"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// geoHandler returns geo information in ipinfo.io-compatible format.
func geoHandler(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if ip == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "could not determine client IP"})
		return
	}

	geo, err := lookupGeo(ip)
	if err != nil {
		log.Printf("geo lookup error for %s: %v", ip, err)
		// return at least the IP even if geo fails
		writeJSON(w, &GeoResponse{IP: ip})
		return
	}

	writeJSON(w, geo)
}

// ─── Healthcheck ─────────────────────────────────────────────

// runHealthcheck performs an HTTP GET to localhost and exits 0/1.
// Used by Docker healthcheck since scratch image has no wget.
func runHealthcheck() {
	resp, err := http.Get("http://127.0.0.1:8080/ip")
	if err != nil {
		os.Exit(1)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		os.Exit(0)
	}
	os.Exit(1)
}

// ─── Main ────────────────────────────────────────────────────

func main() {
	// healthcheck mode for Docker (scratch has no wget)
	if len(os.Args) > 1 && os.Args[1] == "-health" {
		runHealthcheck()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", textHandler)    // plain text single-line (IPv6 preferred if present)
	mux.HandleFunc("/ip", jsonHandler)  // JSON with IP details
	mux.HandleFunc("/json", geoHandler) // JSON with geo info (ipinfo.io style)

	addr := ":8080"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Go IP service listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-done
	log.Println("shutting down…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("stopped")
}
