package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSiteTranslationsMatchGerman(t *testing.T) {
	source := siteStrings["de"]
	for _, code := range siteLanguages {
		for key, value := range siteStrings[code] {
			if _, ok := source[key]; !ok {
				t.Errorf("%s.json has unknown key %q", code, key)
			}
			if strings.Count(value, "<") != strings.Count(source[key], "<") {
				t.Errorf("%s.json %q markup differs from de.json", code, key)
			}
		}
		for key, value := range source {
			got, ok := siteStrings[code][key]
			if !ok {
				t.Errorf("%s.json is missing %q", code, key)
			}
			if value != "" && got == "" {
				t.Errorf("%s.json has empty %q", code, key)
			}
			for _, placeholder := range []string{"{os}", "{d}", "{p}"} {
				if strings.Contains(value, placeholder) != strings.Contains(got, placeholder) {
					t.Errorf("%s.json %q must keep %s", code, key, placeholder)
				}
			}
		}
	}
}

func TestSitePagesRender(t *testing.T) {
	handler := DownloadHandler()
	for _, code := range siteLanguages {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "https://zwoice.example/"+code+"/", nil)
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Language") != code {
			t.Fatalf("%s: status %d, language %q", code, rec.Code, rec.Header().Get("Content-Language"))
		}
		for _, want := range []string{`<html lang="` + code + `">`, `hreflang="x-default" href="https://zwoice.example/"`, `rel="canonical" href="https://zwoice.example/` + code + `/"`, `id="downloadButton"`, `aria-current="page"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: page lacks %s", code, want)
			}
		}
		// {os} stays only in the two data-label templates that download.js fills.
		button := strings.ReplaceAll(siteStrings[code]["downloadFor"], "{os}", "Windows")
		if strings.Count(body, "{os}") != 2 || !strings.Contains(body, ">"+button) {
			t.Errorf("%s: download label not filled", code)
		}
		if strings.Contains(rec.Header().Get("Content-Security-Policy"), "unsafe") {
			t.Errorf("%s: weakened CSP", code)
		}
	}
}

func TestSiteRouting(t *testing.T) {
	handler := DownloadHandler()
	for _, tc := range []struct {
		path, language, location string
		status                   int
	}{
		{"/", "de-AT,de;q=0.9,en;q=0.8", "/de/", http.StatusFound},
		{"/", "fr-CH, fr;q=0.9, en;q=0.8", "/fr/", http.StatusFound},
		{"/", "nb-NO,nb;q=0.9", "/no/", http.StatusFound},
		{"/", "ja,en;q=0.5,nl;q=0.7", "/nl/", http.StatusFound},
		{"/", "de;q=0", "/en/", http.StatusFound},
		{"/", "", "/en/", http.StatusFound},
		{"/pt", "", "/pt/", http.StatusMovedPermanently},
		{"/xx/", "", "", http.StatusNotFound},
		{"/de/extra", "", "", http.StatusNotFound},
		{"/download.html", "", "", http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Accept-Language", tc.language)
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.status || rec.Header().Get("Location") != tc.location {
			t.Errorf("%s (%q): got %d %q, want %d %q", tc.path, tc.language, rec.Code, rec.Header().Get("Location"), tc.status, tc.location)
		}
	}
}

func TestSiteOrigin(t *testing.T) {
	for host, want := range map[string]string{
		"zwoice-production.up.railway.app": "https://zwoice-production.up.railway.app",
		"127.0.0.1:18089":                  "http://127.0.0.1:18089",
		"localhost":                        "http://localhost",
		"[::1]:8080":                       "http://[::1]:8080",
		`evil.example"><script>`:           "",
	} {
		req := httptest.NewRequest(http.MethodGet, "/de/", nil)
		req.Host = host
		if got := siteOrigin(req); got != want {
			t.Errorf("%s: got %q, want %q", host, got, want)
		}
	}
}
