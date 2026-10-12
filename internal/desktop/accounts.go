package desktop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type loginCookie struct{ Server, Token string }

var accountHTTP = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func accountBase(raw string) (string, error) {
	if raw == "" {
		raw = DefaultServerURL
	}
	u, err := BrokerURL(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path = ""
	return u.String(), nil
}
func cookieName(r *http.Request) string {
	return "zwoice_session_" + strings.NewReplacer(":", "_", ".", "_").Replace(r.Host)
}
func sessionToken(r *http.Request, server string) string {
	c, err := r.Cookie(cookieName(r))
	if err != nil {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return ""
	}
	var login loginCookie
	if json.Unmarshal(data, &login) != nil || login.Server != server || len(login.Token) != 43 {
		return ""
	}
	return login.Token
}
func accountCall(ctx context.Context, server, path, token string, body []byte) (map[string]json.RawMessage, int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", server+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := accountHTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode == 204 {
		return nil, 204, nil
	}
	var result map[string]json.RawMessage
	if json.NewDecoder(io.LimitReader(res.Body, 128*1024)).Decode(&result) != nil {
		return nil, res.StatusCode, errors.New("invalid server response")
	}
	return result, res.StatusCode, nil
}
func accountProxy(w http.ResponseWriter, r *http.Request) {
	routes := map[string]string{"request": "/v1/auth/request", "verify": "/v1/auth/verify", "me": "/v1/auth/me", "logout": "/v1/auth/logout", "admin/accounts": "/v1/admin/accounts", "admin/access": "/v1/admin/access", "admin/policy": "/v1/admin/policy"}
	for _, action := range []string{"search", "begin", "confirm", "unlink", "status"} {
		routes["zwift/"+action] = "/v1/zwift/" + action
	}
	routes["admin/observers"] = "/v1/admin/observers"
	action := strings.TrimPrefix(r.URL.Path, "/api/account/")
	path, ok := routes[action]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fail := func(message string, status int) {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"message": message})
	}
	var input struct {
		Server  string          `json:"server"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil {
		fail("Ungültige Anfrage.", 400)
		return
	}
	server, err := accountBase(input.Server)
	if err != nil {
		fail(err.Error(), 400)
		return
	}
	token := sessionToken(r, server)
	clear := func() {
		http.SetCookie(w, &http.Cookie{Name: cookieName(r), Path: "/api/", Value: "", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	if action == "logout" {
		clear()
	}
	if action != "request" && action != "verify" && action != "logout" && token == "" {
		fail("Bitte melde dich bei Zwoice an.", 401)
		return
	}
	if len(input.Payload) == 0 {
		input.Payload = json.RawMessage(`{}`)
	}
	result, status, err := accountCall(r.Context(), server, path, token, input.Payload)
	if err != nil {
		fail("Der Anmeldedienst ist gerade nicht erreichbar. Bitte versuche es erneut.", 503)
		return
	}
	if action == "verify" && status == 200 {
		var fresh string
		var expires int64
		var options struct {
			Remember bool `json:"remember"`
		}
		json.Unmarshal(result["token"], &fresh)
		json.Unmarshal(result["expires"], &expires)
		json.Unmarshal(input.Payload, &options)
		if len(fresh) != 43 || expires <= time.Now().Unix() {
			fail("Ungültige Anmeldung.", 502)
			return
		}
		data, _ := json.Marshal(loginCookie{server, fresh})
		c := &http.Cookie{Name: cookieName(r), Value: base64.RawURLEncoding.EncodeToString(data), Path: "/api/", HttpOnly: true, SameSite: http.SameSiteStrictMode}
		if options.Remember {
			c.MaxAge = int(expires - time.Now().Unix())
			c.Expires = time.Unix(expires, 0)
		}
		http.SetCookie(w, c)
	}
	// The browser never receives the opaque login token, including on malformed responses.
	delete(result, "token")
	if status == 401 && action == "me" {
		clear()
	}
	w.WriteHeader(status)
	if status != 204 {
		json.NewEncoder(w).Encode(result)
	}
}
