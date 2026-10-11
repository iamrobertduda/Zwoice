package desktop

import (
	"net/http/httptest"
	"testing"
)

func TestLocalOriginBoundary(t *testing.T) {
	h := Handler("127.0.0.1:8787")
	for _, tc := range []struct {
		host, origin, path string
		want               int
	}{
		{"evil.example:8787", "", "/", 403},
		{"127.0.0.1:8787", "https://evil.example", "/api/session", 403},
		{"127.0.0.1:8787", "", "/api/session", 403},
		{"127.0.0.1:8787", "", "/", 200},
	} {
		r := httptest.NewRequest("GET", "http://"+tc.host+tc.path, nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%+v: got %d", tc, w.Code)
		}
	}
}
func TestBrokerTransport(t *testing.T) {
	for _, url := range []string{"http://public.example", "https://user:pass@example.com", "https://example.com/?token=secret", "file:///etc/passwd"} {
		if _, err := BrokerURL(url); err == nil {
			t.Fatal("accepted", url)
		}
	}
	for _, url := range []string{"https://example.com", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		if _, err := BrokerURL(url); err != nil {
			t.Fatal(url, err)
		}
	}
}
