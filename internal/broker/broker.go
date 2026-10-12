// Package broker implements a single-replica, in-memory signaling service.
package broker

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"crypto/rand"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"github.com/iamrobertduda/Zwoice/internal/auth"
	"github.com/iamrobertduda/Zwoice/internal/observer"
	"github.com/iamrobertduda/Zwoice/internal/proximity"
	"github.com/iamrobertduda/Zwoice/internal/web"
)

type Config struct {
	Accounts     *accounts.Store
	Observer     *observer.Coordinator
	TURNEndpoint string // Optional transport override for controlled tests.

	Key                []byte
	MaxClients         int
	STUNURLs           []string
	TURNURLs           []string
	TURNSecret         string
	CloudflareKeyID    string
	CloudflareAPIToken string
	TrustRailwayProxy  bool
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
	sessionToken string
	accountID    string
	connectionID string
	checkedAt    time.Time

	conn           *websocket.Conn
	claims         auth.Claims
	rider          proximity.Rider
	out            chan any
	indexed        bool
	cell           proximity.Cell
	fullWarned     bool
	lastPosition   time.Time
	lastPeers      []proximity.Peer
	lastSent       time.Time
	lastWorldTime  int64
	positionFresh  bool
	positionNotice time.Time
}

type Hub struct {
	authBudget         authBudget
	accountConnections map[string]string

	mu          sync.Mutex
	cfg         Config
	clients     map[string]*client
	edges       map[proximity.Edge]bool
	cells       map[proximity.Cell]map[string]struct{}
	joining     chan struct{}
	admissions  map[string]*admission
	pending     map[string]bool
	joinTokens  float64
	joinUpdated time.Time
}

func New(cfg Config) *Hub {
	if cfg.MaxClients <= 0 {
		cfg.MaxClients = 1000
	}
	return &Hub{cfg: cfg, accountConnections: make(map[string]string), clients: make(map[string]*client), edges: make(map[proximity.Edge]bool), cells: make(map[proximity.Cell]map[string]struct{}), joining: make(chan struct{}, 16), admissions: make(map[string]*admission), pending: make(map[string]bool), joinTokens: 60, joinUpdated: time.Now()}
}

func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"zwoice","protocol":2}`))
	})
	mux.HandleFunc("GET /v1/session", h.session)
	mux.HandleFunc("POST /v1/auth/", h.accountHandler)
	mux.HandleFunc("POST /v1/admin/", h.accountHandler)
	mux.HandleFunc("POST /v1/zwift/", h.zwiftHandler)
	if h.cfg.Observer != nil {
		mux.HandleFunc("GET /internal/observer", h.cfg.Observer.Handler)
	}
	mux.Handle("GET /", web.DownloadHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (h *Hub) Run(ctx context.Context) {
	if h.cfg.Observer != nil {
		defer h.cfg.Observer.Close()
	}
	if h.cfg.Accounts != nil {
		go h.revokeLoop(ctx)
	}
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
		if h.cfg.Accounts != nil && now.Sub(c.checkedAt) >= 2*time.Second {
			c.checkedAt = now
			a, _, err := h.cfg.Accounts.Authenticate(c.sessionToken)
			policy, policyErr := h.cfg.Accounts.Policy()
			if err != nil || policyErr != nil || !a.TURNAllowed || !policy.TURNEnabled || a.Zwift == nil || a.Zwift.AthleteID != c.claims.AthleteID {
				c.conn.CloseNow()
				h.removeCell(c)
				delete(h.clients, id)
				continue
			}
		}
		if h.cfg.Observer != nil {
			p, fresh := h.cfg.Observer.Latest(c.claims.AthleteID)
			if fresh && p.Sample.WorldTime > c.lastWorldTime {
				c.lastWorldTime = p.Sample.WorldTime
				h.applyPosition(c, &p.Sample.Position, p.Observed)
				if p.Sample.Name != "" {
					c.rider.Name = cleanName(p.Sample.Name)
				}
			}
			if !fresh {
				h.removeCell(c)
				c.rider.Updated = time.Time{}
			}
			if fresh != c.positionFresh || now.Sub(c.positionNotice) >= 2*time.Second {
				c.positionFresh, c.positionNotice = fresh, now
				h.enqueue(c, map[string]any{"type": "positionSource", "connected": fresh, "athleteId": c.claims.AthleteID, "course": c.rider.Position.Course, "name": c.rider.Name})
			}
		}
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
	// No anonymous or invitation fallback: every voice session needs a valid
	// email-verified account before any TURN credential can be generated.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "use the Zwoice companion", 403)
		return
	}
	if h.cfg.Accounts == nil {
		http.Error(w, "login required", 401)
		return
	}
	sessionToken := bearer(r)
	account, login, err := h.cfg.Accounts.Authenticate(sessionToken)
	if err != nil {
		accountError(w, err)
		return
	}
	if account.Zwift == nil {
		http.Error(w, "link Zwift rider first", 412)
		return
	}
	athleteID := account.Zwift.AthleteID
	if h.cfg.Observer == nil {
		http.Error(w, "observer unavailable", 503)
		return
	}
	claims := auth.Claims{AthleteID: athleteID, Cohort: "public", Expires: login.Expires}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" || r.Header.Get("Sec-WebSocket-Key") == "" {
		http.Error(w, "websocket required", 400)
		return
	}
	id := strconv.FormatInt(claims.AthleteID, 10)
	admitted := false
	connectionID := rand.Text()
	h.mu.Lock()
	if _, exists := h.accountConnections[account.ID]; exists {
		h.mu.Unlock()
		http.Error(w, "account already connected", 409)
		return
	}
	h.accountConnections[account.ID] = connectionID
	h.mu.Unlock()
	defer func() {
		_ = h.cfg.Accounts.RevokeConnection(connectionID)
		h.mu.Lock()
		if h.accountConnections[account.ID] == connectionID {
			delete(h.accountConnections, account.ID)
		}
		h.mu.Unlock()
	}()
	{
		ip := h.admissionIP(r)
		h.mu.Lock()
		status := h.reservePublic(ip, id, time.Now())
		h.mu.Unlock()
		if status != 0 {
			if status == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "15")
			}
			http.Error(w, "please retry shortly or close the other session", status)
			return
		}
		defer func() { h.releasePublic(ip, id, !admitted) }()
	}
	h.mu.Lock()
	_, exists := h.clients[id]
	full := len(h.clients) >= h.cfg.MaxClients
	h.mu.Unlock()
	if exists || full {
		http.Error(w, "already connected or service at capacity", 409)
		return
	}
	releaseObserver, err := h.cfg.Observer.Acquire(athleteID)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	defer releaseObserver()
	select {
	case h.joining <- struct{}{}:
	default:
		http.Error(w, "please retry shortly", 503)
		return
	}
	ice, err := h.fetchICE(r.Context(), id, sessionToken, connectionID)
	<-h.joining
	if err != nil {
		if errors.Is(err, accounts.ErrUnauthorized) || errors.Is(err, accounts.ErrTURN) || errors.Is(err, accounts.ErrDisabled) {
			accountError(w, err)
		} else {
			http.Error(w, "TURN service unavailable", 503)
		}
		return
	}
	// Recheck after the external request; logout/blocking may have raced it.
	current, _, err := h.cfg.Accounts.Authenticate(sessionToken)
	if err != nil {
		accountError(w, err)
		return
	}
	if current.Zwift == nil || current.Zwift.AthleteID != athleteID {
		http.Error(w, "rider link changed", 412)
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
	c := &client{sessionToken: sessionToken, accountID: account.ID, connectionID: connectionID, checkedAt: time.Now(), conn: conn, claims: claims, rider: proximity.Rider{ID: id, Cohort: claims.Cohort, Name: "Rider " + id}, out: make(chan any, 32)}
	c.rider.Name = cleanName(account.Zwift.Name)
	h.enqueue(c, map[string]any{"type": "welcome", "id": id, "iceServers": ice, "maxPeers": proximity.MaxPeers, "protocol": 2})
	h.clients[id] = c
	delete(h.pending, id)
	admitted = true
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(r.Context())
	writerDone := make(chan struct{})
	defer func() {
		cancel()
		conn.CloseNow()
		// A refresh may still be finishing its provider request. Do not mark
		// this connection's grants until its last issuance has finished.
		<-writerDone
		h.mu.Lock()
		if h.clients[id] == c {
			h.removeCell(c)
			delete(h.clients, id)
		}
		h.mu.Unlock()
	}()
	go func() {
		defer close(writerDone)
		defer cancel()
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		refresh := time.NewTimer(30 * time.Minute)
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
				servers, err := h.fetchICE(ctx, id, sessionToken, connectionID)
				if err != nil {
					if errors.Is(err, accounts.ErrUnauthorized) || errors.Is(err, accounts.ErrTURN) || errors.Is(err, accounts.ErrDisabled) {
						return
					}
					refresh.Reset(time.Minute)
					continue
				}
				h.enqueue(c, map[string]any{"type": "ice", "iceServers": servers})
				refresh.Reset(30 * time.Minute)
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

func cleanName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if n := []rune(name); len(n) > 32 {
		name = string(n[:32])
	}
	return name
}

func (h *Hub) handle(c *client, m Message, now time.Time) {
	switch m.Type {
	case "voice":
		c.rider.Active = m.Active
		if !m.Active {
			h.removeCell(c)
		}
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
		if len(m.Payload) > 0 && len(m.Payload) <= 20*1024 && json.Valid(m.Payload) {
			h.enqueue(target, map[string]any{"type": "signal", "from": c.rider.ID, "payload": m.Payload})
		}
	}
}

func (h *Hub) applyPosition(c *client, position *proximity.Position, now time.Time) {
	if position == nil {
		h.removeCell(c)
		c.rider.Updated = time.Time{}
		return
	}
	if !position.Valid() {
		return
	}
	c.lastPosition = now
	// A malicious client cannot force unbounded grid density.
	next := c.rider
	next.Position = *position
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
		username := fmt.Sprintf("%d:%s", now.Add(time.Hour).Unix(), id)
		mac := hmac.New(sha1.New, []byte(h.cfg.TURNSecret))
		mac.Write([]byte(username))
		servers = append(servers, map[string]any{"urls": h.cfg.TURNURLs, "username": username, "credential": base64.StdEncoding.EncodeToString(mac.Sum(nil))})
	}
	return servers
}
