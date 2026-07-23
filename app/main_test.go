package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSafeGeoText(t *testing.T) {
	valid := []string{
		"",
		"Kyiv",
		"Frankfurt am Main",
		"Zürich",
		"São Paulo",
		"AS15169 Google LLC",
		"AS35819 Etihad Etisalat Company (Mobily) - Saudi Arabia",
	}
	for _, s := range valid {
		if !safeGeoText(s) {
			t.Errorf("safeGeoText(%q) = false, want true", s)
		}
	}

	invalid := []string{
		"Kyiv\nX-Injected: 1",
		"Kyiv\x00",
		"\x1b[31mKyiv",
		strings.Repeat("a", geoMaxTextLen+1),
	}
	for _, s := range invalid {
		if safeGeoText(s) {
			t.Errorf("safeGeoText(%q) = true, want false", s)
		}
	}
}

func TestSafeGeoCountryCode(t *testing.T) {
	for _, s := range []string{"", "UA", "DE"} {
		if !safeGeoCountryCode(s) {
			t.Errorf("safeGeoCountryCode(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"ua", "USA", "U", "U1", "<b>"} {
		if safeGeoCountryCode(s) {
			t.Errorf("safeGeoCountryCode(%q) = true, want false", s)
		}
	}
}

func TestSafeGeoTimezone(t *testing.T) {
	for _, s := range []string{"", "Europe/Kyiv", "America/Argentina/Buenos_Aires", "Etc/GMT+5"} {
		if !safeGeoTimezone(s) {
			t.Errorf("safeGeoTimezone(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"Europe/Kyiv\n", "Europe Kyiv; rm -rf", strings.Repeat("a", geoMaxTimezoneLen+1)} {
		if safeGeoTimezone(s) {
			t.Errorf("safeGeoTimezone(%q) = true, want false", s)
		}
	}
}

func TestHTTPClientDoesNotFollowRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect was followed to the attacker-chosen target")
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	resp, err := httpClient.Get(origin.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
}
