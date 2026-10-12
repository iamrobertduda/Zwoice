package broker

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"github.com/iamrobertduda/Zwoice/internal/observer"
)

type authBudget struct {
	mu      sync.Mutex
	ips     map[string]*admission
	tokens  float64
	updated time.Time
}

func (h *Hub) allowAuth(r *http.Request) bool {
	now := time.Now()
	g := &h.authBudget
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ips == nil {
		g.ips = map[string]*admission{}
		g.tokens = 60
		g.updated = now
	}
	for ip, e := range g.ips {
		if now.Sub(e.updated) > 10*time.Minute {
			delete(g.ips, ip)
		}
	}
	ip := h.admissionIP(r)
	e := g.ips[ip]
	if e == nil {
		if len(g.ips) >= 10000 {
			return false
		}
		e = &admission{tokens: 30, updated: now}
		g.ips[ip] = e
	}
	e.tokens = min(30, e.tokens+now.Sub(e.updated).Seconds()/2)
	e.updated = now
	g.tokens = min(60, g.tokens+now.Sub(g.updated).Seconds()*2)
	g.updated = now
	cost := float64(1)
	if r.URL.Path == "/v1/zwift/status" || r.URL.Path == "/v1/auth/me" {
		cost = 0.1
	}
	if r.URL.Path == "/v1/auth/request" {
		cost = 6
	}
	if e.tokens < cost || g.tokens < cost {
		return false
	}
	e.tokens -= cost
	g.tokens -= cost
	return true
}
func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(value, "Bearer ")
}
func jsonResult(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func accountError(w http.ResponseWriter, err error) {
	status, message := http.StatusBadRequest, err.Error()
	switch {
	case errors.Is(err, accounts.ErrUnauthorized):
		status = 401
	case errors.Is(err, accounts.ErrDisabled) || errors.Is(err, accounts.ErrTURN):
		status = 403
	case errors.Is(err, accounts.ErrRate):
		status = 429
		w.Header().Set("Retry-After", "60")
	case errors.Is(err, accounts.ErrMail):
		status = 503
	case errors.Is(err, observer.ErrUnavailable), errors.Is(err, observer.ErrCapacity):
		status = 503
	case errors.Is(err, accounts.ErrZwiftClaimed):
		status = 409
	}
	jsonResult(w, status, map[string]any{"message": message})
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		accountError(w, errors.New("Ungültige Anfrage."))
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		accountError(w, errors.New("Ungültige Anfrage."))
		return false
	}
	return true
}
func (h *Hub) accountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "use the Zwoice companion", 403)
		return
	}
	if h.cfg.Accounts == nil {
		accountError(w, accounts.ErrMail)
		return
	}
	if !h.allowAuth(r) {
		accountError(w, accounts.ErrRate)
		return
	}
	store := h.cfg.Accounts
	token := bearer(r)
	switch r.URL.Path {
	case "/v1/auth/request":
		var input struct {
			Email string `json:"email"`
		}
		if !decode(w, r, &input) {
			return
		}
		id, err := store.RequestCode(r.Context(), input.Email)
		if err != nil {
			accountError(w, err)
			return
		}
		jsonResult(w, 200, map[string]any{"challenge": id, "message": "Wenn die Anmeldung möglich ist, findest du deinen Code gleich in deinem Postfach."})
	case "/v1/auth/verify":
		var input struct {
			Challenge string `json:"challenge"`
			Code      string `json:"code"`
			Remember  bool   `json:"remember"`
		}
		if !decode(w, r, &input) {
			return
		}
		token, a, expires, err := store.VerifyCode(input.Challenge, input.Code, input.Remember)
		if err != nil {
			accountError(w, err)
			return
		}
		jsonResult(w, 200, map[string]any{"token": token, "account": a, "expires": expires})
	case "/v1/auth/me":
		a, session, err := store.Authenticate(token)
		if err != nil {
			accountError(w, err)
			return
		}
		p, err := store.Policy()
		if err != nil {
			accountError(w, accounts.ErrMail)
			return
		}
		jsonResult(w, 200, map[string]any{"account": a, "expires": session.Expires, "policy": p})
	case "/v1/auth/logout":
		if err := store.Logout(token); err != nil {
			accountError(w, accounts.ErrMail)
			return
		}
		w.WriteHeader(204)
	case "/v1/admin/accounts":
		var input struct {
			Cursor string `json:"cursor"`
		}
		if !decode(w, r, &input) {
			return
		}
		list, next, err := store.List(token, input.Cursor)
		if err != nil {
			accountError(w, err)
			return
		}
		p, err := store.Policy()
		if err != nil {
			accountError(w, accounts.ErrMail)
			return
		}
		jsonResult(w, 200, map[string]any{"accounts": list, "next": next, "policy": p})
	case "/v1/admin/observers":
		a, _, err := store.Authenticate(token)
		if err != nil {
			accountError(w, err)
			return
		}
		if !a.Admin {
			http.Error(w, "admin required", 403)
			return
		}
		var status observer.Status
		if h.cfg.Observer != nil {
			status = h.cfg.Observer.Status()
		}
		jsonResult(w, 200, status)
	case "/v1/admin/access":
		var input struct {
			AccountID   string `json:"accountId"`
			Disabled    bool   `json:"disabled"`
			TURNAllowed bool   `json:"turnAllowed"`
			TURNLimit   int    `json:"turnLimit"`
		}
		if !decode(w, r, &input) {
			return
		}
		if err := store.SetAccess(token, input.AccountID, input.Disabled, input.TURNAllowed, input.TURNLimit); err != nil {
			accountError(w, err)
			return
		}
		jsonResult(w, 200, map[string]bool{"ok": true})
	case "/v1/admin/policy":
		var input accounts.Policy
		if !decode(w, r, &input) {
			return
		}
		if err := store.SetPolicy(token, input); err != nil {
			accountError(w, err)
			return
		}
		jsonResult(w, 200, map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}
