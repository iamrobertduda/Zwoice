// Package desktop bridges the loopback browser UI to the hosted voice service.
package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"github.com/iamrobertduda/Zwoice/internal/broker"
	"github.com/iamrobertduda/Zwoice/internal/web"
)

type Start struct {
	Server string `json:"server"`
}

// Overridable by release builders; no credential is ever embedded here.
var DefaultServerURL = "https://zwoice-production.up.railway.app"

func BrokerURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("ungültige Server-Adresse")
	}
	ip := net.ParseIP(u.Hostname())
	local := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return nil, errors.New("für den Sprachserver ist HTTPS erforderlich")
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/v1/session"
	return u, nil
}

func Handler(host string, shutdown ...func()) http.Handler {
	var active atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /config.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"server": DefaultServerURL})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"service":"zwoice-desktop","protocol":2}`))
	})
	mux.HandleFunc("POST /api/quit", func(w http.ResponseWriter, r *http.Request) {
		if len(shutdown) == 0 {
			http.Error(w, "managed test session", 409)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		go shutdown[0]()
	})
	mux.HandleFunc("POST /api/setup", setup)
	mux.HandleFunc("POST /api/account/", accountProxy)
	mux.Handle("GET /", web.Handler())
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		if !active.CompareAndSwap(false, true) {
			http.Error(w, "Zwoice ist bereits in einem anderen Tab aktiv", 409)
			return
		}
		defer active.Store(false)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(24 * 1024)
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		write := func(v any) error {
			c, s := context.WithTimeout(ctx, 3*time.Second)
			defer s()
			return wsjson.Write(c, conn, v)
		}
		fail := func(message string) { write(map[string]any{"type": "error", "message": message}) }
		var start Start
		c, s := context.WithTimeout(ctx, 5*time.Second)
		err = wsjson.Read(c, conn, &start)
		s()
		if err != nil {
			return
		}
		if start.Server == "" {
			start.Server = DefaultServerURL
		}
		remoteURL, err := BrokerURL(start.Server)
		if err != nil {
			fail(err.Error())
			return
		}
		base, _ := accountBase(start.Server)
		token := sessionToken(r, base)
		if token == "" {
			write(map[string]any{"type": "error", "fatal": true, "loginRequired": true, "message": "Bitte melde dich zuerst bei Zwoice an."})
			return
		}
		// Account and TURN requests can take longer than the browser watchdog.
		// Keep transport liveness separate from the rider's position freshness.
		pulseCtx, stopPulse := context.WithCancel(ctx)
		defer stopPulse()
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-pulseCtx.Done():
					return
				case <-ticker.C:
					if write(map[string]any{"type": "heartbeat"}) != nil {
						cancel()
						return
					}
				}
			}
		}()
		accountResponse, status, loginErr := accountCall(ctx, base, "/v1/auth/me", token, []byte(`{}`))
		if loginErr != nil || status != 200 {
			write(map[string]any{"type": "error", "fatal": status == 401 || status == 403, "loginRequired": status == 401 || status == 403, "message": "Deine Anmeldung konnte nicht bestätigt werden. Bitte melde dich erneut an oder prüfe deine Verbindung."})
			return
		}
		var account accounts.Account
		if json.Unmarshal(accountResponse["account"], &account) != nil || account.Zwift == nil {
			write(map[string]any{"type": "error", "fatal": true, "linkRequired": true, "message": "Verknüpfe zuerst deinen Zwift-Fahrer."})
			return
		}
		// Closing the browser cancels pending work and releases this tab’s slot.
		messages := make(chan broker.Message, 32)
		go func() {
			defer cancel()
			for {
				var m broker.Message
				if wsjson.Read(ctx, conn, &m) != nil {
					return
				}
				select {
				case messages <- m:
				case <-ctx.Done():
					return
				}
			}
		}()
		write(map[string]any{"type": "setup", "state": "connecting", "message": "Verbinde deinen Fahrer …"})
		c, s = context.WithTimeout(ctx, 8*time.Second)
		remote, res, err := websocket.Dial(c, remoteURL.String(), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
		s()
		if err != nil {
			message := "Sprachserver nicht erreichbar. Adresse und Verbindung prüfen."
			if res != nil && res.StatusCode == 401 {
				message = "Deine Anmeldung ist abgelaufen. Bitte melde dich erneut an."
			}
			if res != nil && res.StatusCode == 403 {
				message = "Sprachchat ist für dein Konto gesperrt oder dein Tageslimit ist erreicht. Bitte wende dich an den Betreiber."
			}
			if res != nil && res.StatusCode == 409 {
				message = "Deine Fahrt ist bereits in einem anderen Zwoice-Fenster verbunden. Schließe das andere Fenster."
			}
			if res != nil && res.StatusCode == 412 {
				message = "Bitte verknüpfe deinen Zwift-Fahrer erneut."
			}
			if res != nil && res.StatusCode == 503 {
				message = "Der Sprachdienst ist gerade ausgelastet oder vorübergehend nicht verfügbar. Zwoice versucht es automatisch erneut."
			}
			if res != nil && res.StatusCode == 429 {
				message = "Ein kurzer Moment Pause. Zwoice verbindet sich gleich automatisch erneut."
			}
			retryAfter := 0
			if res != nil && res.StatusCode == 429 {
				retryAfter = 15
			}
			write(map[string]any{"type": "error", "fatal": res != nil && (res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 412), "loginRequired": res != nil && res.StatusCode == 401, "linkRequired": res != nil && res.StatusCode == 412, "retryAfter": retryAfter, "message": message})
			return
		}
		defer remote.CloseNow()
		remote.SetReadLimit(32 * 1024)
		send := func(v any) error {
			c, s := context.WithTimeout(ctx, 3*time.Second)
			defer s()
			return wsjson.Write(c, remote, v)
		}
		var welcome struct {
			Type     string          `json:"type"`
			ID       string          `json:"id"`
			ICE      json.RawMessage `json:"iceServers"`
			MaxPeers int             `json:"maxPeers"`
			Protocol int             `json:"protocol"`
		}
		c, s = context.WithTimeout(ctx, 5*time.Second)
		err = wsjson.Read(c, remote, &welcome)
		s()
		if err != nil || welcome.Type != "welcome" || welcome.Protocol != 2 {
			fail("Inkompatibler Sprachserver")
			return
		}
		write(welcome)
		stopPulse()
		go func() {
			defer cancel()
			for {
				var message json.RawMessage
				if wsjson.Read(ctx, remote, &message) != nil {
					return
				}
				if write(message) != nil {
					return
				}
			}
		}()
		for {
			var m broker.Message
			select {
			case <-ctx.Done():
				return
			case m = <-messages:
			}
			// The browser can control voice and signaling, never its own position.
			if m.Type == "voice" || m.Type == "signal" {
				if send(m) != nil {
					return
				}
			}
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exact Host + Origin checks protect the loopback API against DNS rebinding
		// and drive-by websites connecting to local services.
		if r.Host != host {
			http.Error(w, "invalid host", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			http.Error(w, "invalid origin", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Origin") != "http://"+host {
			http.Error(w, "origin required", 403)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; media-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}
