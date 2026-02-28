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
	geoCache   = make(map[string]*geoEntry)
	geoMu      sync.RWMutex
	geoTTL     = 5 * time.Minute
	httpClient = &http.Client{Timeout: 3 * time.Second}
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

// lookupGeo fetches geo data from ip-api.com with in-memory caching.
func lookupGeo(ip string) (*GeoResponse, error) {
	// check cache
	geoMu.RLock()
	if entry, ok := geoCache[ip]; ok && time.Since(entry.fetchedAt) < geoTTL {
		geoMu.RUnlock()
		return entry.data, nil
	}
	geoMu.RUnlock()

	// fetch from ip-api.com (free, no key, HTTP only)
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,countryCode,regionName,city,lat,lon,timezone,isp,org,as,query", ip)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("geo lookup failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "could not determine client IP"})
		return
	}

	geo, err := lookupGeo(ip)
	if err != nil {
		log.Printf("geo lookup error for %s: %v", ip, err)
		// return at least the IP even if geo fails
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&GeoResponse{IP: ip})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(geo)
}

// ─── Main ────────────────────────────────────────────────────

func main() {
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
