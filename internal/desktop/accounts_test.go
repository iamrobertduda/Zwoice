package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginTokenStaysInServerBoundHTTPOnlyCookie(t *testing.T) {
	token := strings.Repeat("s", 43)
	seenToken := ""
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenToken = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{"token": token, "expires": time.Now().Add(time.Hour).Unix(), "account": map[string]string{"email": "rider@example.test"}})
	}))
	defer remote.Close()
	handler := Handler("127.0.0.1:8787")
	call := func(action, server string, cookie *http.Cookie) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"server": server, "payload": map[string]bool{"remember": true}})
		r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/account/"+action, strings.NewReader(string(body)))
		r.Header.Set("Origin", "http://127.0.0.1:8787")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	res := call("verify", remote.URL, nil)
	if res.Code != 200 || strings.Contains(res.Body.String(), token) {
		t.Fatal("token leaked to JS", res.Code)
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge <= 0 {
		t.Fatal("unsafe cookie")
	}
	res = call("me", remote.URL, cookies[0])
	if res.Code != 200 || seenToken != "Bearer "+token || strings.Contains(res.Body.String(), token) {
		t.Fatal("session did not reach intended server")
	}
	seenToken = ""
	res = call("me", "http://127.0.0.1:1", cookies[0])
	if res.Code != 401 || seenToken != "" {
		t.Fatal("cookie forwarded to different server")
	}
	res = call("logout", remote.URL, cookies[0])
	if res.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout kept local session")
	}
}
