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
	mux.HandleFunc("POST /api/setup", setup)
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
		if start.Sauce == "" {
			start.Sauce = "http://127.0.0.1:1080"
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
		// Read the browser from the beginning so closing/cancelling while Sauce
		// is absent cancels every pending request and releases this tab's slot.
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
		samples := make(chan sauce.Sample, 1)
		statuses := make(chan string, 1)
		go sauce.Run(ctx, start.Sauce, func(sample sauce.Sample) {
			select {
			case <-samples:
			default:
			}
			select {
			case samples <- sample:
			default:
			}
		}, func(message string) {
			select {
			case <-statuses:
			default:
			}
			select {
			case statuses <- message:
			default:
			}
		})
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
		write(map[string]any{"type": "setup", "state": "waiting", "message": "Starte deine Fahrt in Zwift. Zwoice verbindet dich automatisch."})
		var first sauce.Sample
	waitForRide:
		for {
			select {
			case <-ctx.Done():
				return
			case first = <-samples:
				break waitForRide
			case <-statuses:
				write(map[string]any{"type": "setup", "state": "missing", "message": "Öffne Sauce for Zwift. Sobald deine Fahrt erkannt wird, geht es automatisch weiter."})
			}
		}
		write(map[string]any{"type": "setup", "state": "connecting", "name": first.Name, "message": "Fahrt erkannt. Verbinde deinen Sprachchat …"})
		c, s = context.WithTimeout(ctx, 8*time.Second)
		remote, res, err := websocket.Dial(c, remoteURL.String(), &websocket.DialOptions{HTTPHeader: http.Header{"Zwoice-Athlete-ID": []string{strconv.FormatInt(first.AthleteID, 10)}}})
		s()
		if err != nil {
			message := "Sprachserver nicht erreichbar. Adresse und Verbindung prüfen."
			if res != nil && res.StatusCode == 401 {
				message = "Diese Zwoice-Version wird vom Server nicht unterstützt. Bitte die aktuelle Version installieren."
			}
			if res != nil && res.StatusCode == 409 {
				message = "Deine Fahrt ist bereits in einem anderen Zwoice-Fenster verbunden. Schließe das andere Fenster."
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
			write(map[string]any{"type": "error", "fatal": res != nil && res.StatusCode == 401, "retryAfter": retryAfter, "message": message})
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
		stopPulse()
		write(welcome)
		name := strings.TrimSpace(start.Name)
		if name == "" {
			name = first.Name
		}
		send(broker.Message{Type: "hello", Name: name})
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
		// Send the seed immediately. Later samples are coalesced to four/second.
		send(broker.Message{Type: "position", Position: &first.Position})
		write(map[string]any{"type": "sauce", "connected": true, "athleteId": first.AthleteID, "course": first.Position.Course, "name": name})
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			last := time.Now()
			stale := false
			for {
				select {
				case <-ctx.Done():
					return
				case <-statuses:
					send(broker.Message{Type: "position"})
					write(map[string]any{"type": "sauce", "connected": false, "message": "Sauce-Verbindung unterbrochen. Zwoice wartet auf deine Fahrt."})
					stale = true
				case <-ticker.C:
					select {
					case sample := <-samples:
						if strconv.FormatInt(sample.AthleteID, 10) != welcome.ID {
							write(map[string]any{"type": "error", "fatal": false, "message": "Neuer Fahrer erkannt. Zwoice verbindet deine neue Fahrt automatisch."})
							cancel()
							return
						}
						if start.Name == "" && sample.Name != "" && sample.Name != name {
							name = sample.Name
							send(broker.Message{Type: "hello", Name: name})
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
