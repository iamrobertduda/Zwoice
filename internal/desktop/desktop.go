// Package desktop bridges a loopback browser UI to Sauce and the hosted broker.
package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/broker"
	"github.com/iamrobertduda/Zwoice/internal/sauce"
	"github.com/iamrobertduda/Zwoice/internal/web"
)

type Start struct {
	Server string `json:"server"`
	Sauce  string `json:"sauce"`
	Token  string `json:"token"`
	Name   string `json:"name"`
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
		w.Write([]byte(`{"service":"zwoice-desktop","protocol":1}`))
	})
	mux.HandleFunc("POST /api/quit", func(w http.ResponseWriter, r *http.Request) {
		if len(shutdown) == 0 {
			http.Error(w, "managed test session", 409)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		go shutdown[0]()
	})
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
		remoteURL, err := BrokerURL(start.Server)
		if err != nil {
			fail(err.Error())
			return
		}
		if _, err = sauce.ValidateURL(start.Sauce); err != nil {
			fail(err.Error())
			return
		}
		c, s = context.WithTimeout(ctx, 8*time.Second)
		remote, res, err := websocket.Dial(c, remoteURL.String(), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + start.Token}}})
		s()
		if err != nil {
			message := "Sprachserver nicht erreichbar. Adresse und Verbindung prüfen."
			if res != nil && res.StatusCode == 401 {
				message = "Einladung ungültig oder abgelaufen."
			}
			if res != nil && res.StatusCode == 409 {
				message = "Dieser Fahrer ist bereits verbunden oder der Server ist voll."
			}
			if res != nil && res.StatusCode == 503 {
				message = "Der TURN-Dienst ist momentan nicht verfügbar. Bitte später erneut verbinden."
			}
			write(map[string]any{"type": "error", "fatal": res != nil && res.StatusCode == 401, "message": message})
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
		if err != nil || welcome.Type != "welcome" || welcome.Protocol != 1 {
			fail("Inkompatibler Sprachserver")
			return
		}
		write(welcome)
		send(broker.Message{Type: "hello", Name: start.Name})
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
		// Coalesce Sauce to four updates/second, independent of its packet rate.
		samples := make(chan sauce.Sample, 1)
		go sauce.Run(ctx, start.Sauce, func(sample sauce.Sample) {
			select {
			case samples <- sample:
			default:
				select {
				case <-samples:
				default:
				}
				select {
				case samples <- sample:
				default:
				}
			}
		}, func(message string) {
			send(broker.Message{Type: "position"})
			write(map[string]any{"type": "sauce", "connected": false, "message": message})
		})
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			var last time.Time
			stale := true
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					select {
					case sample := <-samples:
						if strconv.FormatInt(sample.AthleteID, 10) != welcome.ID {
							fail("Die Einladung gehört zu einer anderen Zwift-ID als Sauce /self.")
							cancel()
							return
						}
						last = time.Now()
						stale = false
						if send(broker.Message{Type: "position", Position: &sample.Position}) != nil {
							cancel()
							return
						}
						write(map[string]any{"type": "sauce", "connected": true, "athleteId": sample.AthleteID, "course": sample.Position.Course})
					default:
					}
					if !stale && time.Since(last) > 4*time.Second {
						stale = true
						send(broker.Message{Type: "position"})
						write(map[string]any{"type": "sauce", "connected": false, "message": "Keine frischen Positionsdaten. Sprache pausiert automatisch."})
					}
				}
			}
		}()
		for {
			var m broker.Message
			if wsjson.Read(ctx, conn, &m) != nil {
				return
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
