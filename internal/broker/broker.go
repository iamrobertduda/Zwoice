// Package broker implements a single-replica, in-memory signaling service.
package broker

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/auth"
	"github.com/iamrobertduda/Zwoice/internal/proximity"
)

type Config struct {
	Key                []byte
	MaxClients         int
	STUNURLs           []string
	TURNURLs           []string
	TURNSecret         string
	CloudflareKeyID    string
	CloudflareAPIToken string
}

type Message struct {
	Type     string              `json:"type"`
	Name     string              `json:"name,omitempty"`
	Active   bool                `json:"active"`
	Position *proximity.Position `json:"position"`
	To       string              `json:"to,omitempty"`
	Payload  json.RawMessage     `json:"payload,omitempty"`
}

type client struct {
	conn         *websocket.Conn
	claims       auth.Claims
	rider        proximity.Rider
	out          chan any
	indexed      bool
	cell         proximity.Cell
	fullWarned   bool
	lastPosition time.Time
	lastPeers    []proximity.Peer
	lastSent     time.Time
}

type Hub struct {
	mu      sync.Mutex
	cfg     Config
	clients map[string]*client
	edges   map[proximity.Edge]bool
	cells   map[proximity.Cell]map[string]struct{}
	joining chan struct{}
}

func New(cfg Config) *Hub {
	if cfg.MaxClients <= 0 {
		cfg.MaxClients = 1000
	}
	return &Hub{cfg: cfg, clients: make(map[string]*client), edges: make(map[proximity.Edge]bool), cells: make(map[proximity.Cell]map[string]struct{}), joining: make(chan struct{}, 16)}
}

func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"zwoice","protocol":1}`))
	})
	mux.HandleFunc("GET /v1/session", h.session)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	defer func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, c := range h.clients {
			c.conn.CloseNow()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			h.tick(now)
		}
	}
}

func (h *Hub) riders() map[string]proximity.Rider {
	m := make(map[string]proximity.Rider, len(h.clients))
	for id, c := range h.clients {
		m[id] = c.rider
	}
	return m
}

func (h *Hub) tick(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, c := range h.clients {
		if now.Sub(c.rider.Updated) >= proximity.StaleAfter {
			h.removeCell(c)
		}
		if c.claims.Expires <= now.Unix() {
			h.removeCell(c)
			c.conn.CloseNow()
			delete(h.clients, id)
		}
	}
	peers, edges := proximity.Build(h.riders(), h.edges, now)
	h.edges = edges
	for id, list := range peers {
		c := h.clients[id]
		if slices.Equal(c.lastPeers, list) && now.Sub(c.lastSent) < 2*time.Second {
			continue
		}
		c.lastPeers = append(c.lastPeers[:0], list...)
		c.lastSent = now
		h.enqueue(c, map[string]any{"type": "peers", "peers": list})
	}
}

func (h *Hub) enqueue(c *client, value any) {
	if c == nil {
		return
	}
	select {
	case c.out <- value:
	default:
		c.conn.CloseNow()
	} // slow consumers cannot grow memory
}

func (h *Hub) session(w http.ResponseWriter, r *http.Request) {
	// Only the local companion authenticates here, using an HTTP header. Tokens
	// never appear in URLs, proxy access logs, or browser WebSocket protocols.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "use the Zwoice companion", 403)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	claims, err := auth.Verify(h.cfg.Key, token, time.Now())
	if err != nil {
		http.Error(w, "invalid or expired invitation", 401)
		return
	}
	id := strconv.FormatInt(claims.AthleteID, 10)
	h.mu.Lock()
	_, exists := h.clients[id]
	full := len(h.clients) >= h.cfg.MaxClients
	h.mu.Unlock()
	if exists || full {
		http.Error(w, "already connected or service at capacity", 409)
		return
	}
	select {
	case h.joining <- struct{}{}:
	default:
		http.Error(w, "please retry shortly", 503)
		return
	}
	ice, err := h.fetchICE(r.Context(), id)
	<-h.joining
	if err != nil {
		http.Error(w, "TURN service unavailable", 503)
		return
	}
	h.mu.Lock()
	if _, exists := h.clients[id]; exists || len(h.clients) >= h.cfg.MaxClients {
		h.mu.Unlock()
		http.Error(w, "already connected or service at capacity", 409)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.mu.Unlock()
		return
	}
	conn.SetReadLimit(24 * 1024)
	c := &client{conn: conn, claims: claims, rider: proximity.Rider{ID: id, Cohort: claims.Cohort, Name: "Rider " + id}, out: make(chan any, 32)}
	h.enqueue(c, map[string]any{"type": "welcome", "id": id, "iceServers": ice, "maxPeers": proximity.MaxPeers, "protocol": 1})
	h.clients[id] = c
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(r.Context())
	defer func() {
		cancel()
		conn.CloseNow()
		h.mu.Lock()
		if h.clients[id] == c {
			h.removeCell(c)
			delete(h.clients, id)
		}
		h.mu.Unlock()
	}()
	go func() {
		defer cancel()
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		refresh := time.NewTimer(6 * time.Hour)
		defer refresh.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case value := <-c.out:
				writeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
				err := wsjson.Write(writeCtx, conn, value)
				stop()
				if err != nil {
					return
				}
			case <-heartbeat.C:
				pingCtx, stop := context.WithTimeout(ctx, 8*time.Second)
				err := conn.Ping(pingCtx)
				stop()
				if err != nil {
					return
				}
			case <-refresh.C:
				servers, err := h.fetchICE(ctx, id)
				if err != nil {
					refresh.Reset(time.Minute)
					continue
				}
				h.enqueue(c, map[string]any{"type": "ice", "iceServers": servers})
				refresh.Reset(6 * time.Hour)
			}
		}
	}()
	window, tokens := time.Now(), 100
	for {
		var msg Message
		if wsjson.Read(ctx, conn, &msg) != nil {
			return
		}
		now := time.Now()
		if now.Sub(window) >= time.Second {
			window, tokens = now, 100
		}
		tokens--
		if tokens < 0 {
			return
		}
		h.mu.Lock()
		h.handle(c, msg, now)
		h.mu.Unlock()
	}
}

func (h *Hub) handle(c *client, m Message, now time.Time) {
	switch m.Type {
	case "hello":
		name := strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, m.Name))
		if n := []rune(name); len(n) > 32 {
			name = string(n[:32])
		}
		if name != "" {
			c.rider.Name = name
		}
	case "voice":
		c.rider.Active = m.Active
		if !m.Active {
			h.removeCell(c)
		}
	case "position":
		if m.Position == nil {
			h.removeCell(c)
			c.rider.Updated = time.Time{}
			return
		}
		if !m.Position.Valid() {
			return
		}
		if now.Sub(c.lastPosition) < 200*time.Millisecond {
			return
		}
		c.lastPosition = now
		// A malicious client cannot force unbounded grid density.
		next := c.rider
		next.Position = *m.Position
		next.Updated = now
		key := proximity.CellFor(next)
		if next.Active && (!c.indexed || c.cell != key) && len(h.cells[key]) >= proximity.MaxCellOccupancy {
			h.removeCell(c)
			c.rider.Updated = time.Time{}
			if !c.fullWarned {
				h.enqueue(c, map[string]any{"type": "error", "fatal": false, "message": "Diese lokale Gruppe ist voll. Bitte etwas Abstand gewinnen."})
				c.fullWarned = true
			}
			return
		}
		c.fullWarned = false
		if c.indexed && c.cell != key {
			h.removeCell(c)
		}
		if next.Active && !c.indexed {
			if h.cells[key] == nil {
				h.cells[key] = make(map[string]struct{})
			}
			h.cells[key][next.ID] = struct{}{}
			c.cell = key
			c.indexed = true
		}
		c.rider = next
	case "signal":
		target := h.clients[m.To]
		if target == nil || !proximity.Near(c.rider, target.rider, now, proximity.LeaveRadius) {
			return
		}
		a, b := c.rider.ID, m.To
		if a > b {
			a, b = b, a
		}
		if !h.edges[proximity.Edge{A: a, B: b}] {
			return
		}
		// The payload remains opaque SDP/ICE; authorization is the current edge.
		if len(m.Payload) > 0 && len(m.Payload) <= 20*1024 && json.Valid(m.Payload) {
			h.enqueue(target, map[string]any{"type": "signal", "from": c.rider.ID, "payload": m.Payload})
		}
	}
}

func (h *Hub) removeCell(c *client) {
	if !c.indexed {
		return
	}
	delete(h.cells[c.cell], c.rider.ID)
	if len(h.cells[c.cell]) == 0 {
		delete(h.cells, c.cell)
	}
	c.indexed = false
}

// Coturn REST credentials: short-lived HMAC-SHA1 credentials (the TURN REST
// convention, not a password hash). Refreshed before expiry for multi-hour rides.
func (h *Hub) iceServers(id string, now time.Time) []map[string]any {
	servers := []map[string]any{}
	if len(h.cfg.STUNURLs) > 0 {
		servers = append(servers, map[string]any{"urls": h.cfg.STUNURLs})
	}
	if len(h.cfg.TURNURLs) > 0 && h.cfg.TURNSecret != "" {
		username := fmt.Sprintf("%d:%s", now.Add(12*time.Hour).Unix(), id)
		mac := hmac.New(sha1.New, []byte(h.cfg.TURNSecret))
		mac.Write([]byte(username))
		servers = append(servers, map[string]any{"urls": h.cfg.TURNURLs, "username": username, "credential": base64.StdEncoding.EncodeToString(mac.Sum(nil))})
	}
	return servers
}
