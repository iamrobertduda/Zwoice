// Package observer coordinates a private pool of server-side Sauce processes.
// Clients can never publish positions or select another account's rider.
package observer

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/sauce"
)

var ErrUnavailable = errors.New("Die Fahrer-Verbindung ist gerade nicht verfügbar. Bitte versuche es gleich erneut.")
var ErrCapacity = errors.New("Alle Fahrer-Verbindungen sind gerade belegt. Bitte versuche es in einem Moment erneut.")

const FreshFor = 3 * time.Second

type Profile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type Lease struct {
	AthleteID int64  `json:"athleteId"`
	ID        string `json:"lease"`
}
type Position struct {
	Lease      string        `json:"lease"`
	Athlete    sauce.Athlete `json:"athlete"`
	ObservedAt int64         `json:"observedAt"` // worker wall clock, before network transit
}
type Frame struct {
	Type      string    `json:"type"`
	ID        string    `json:"id,omitempty"`
	Capacity  int       `json:"capacity,omitempty"`
	Leases    []Lease   `json:"leases,omitempty"`
	Query     string    `json:"query,omitempty"`
	AthleteID int64     `json:"athleteId,omitempty"`
	Profiles  []Profile `json:"profiles,omitempty"`
	Position  *Position `json:"position,omitempty"`
	Error     string    `json:"error,omitempty"`
	Health    *Health   `json:"health,omitempty"`
}
type Snapshot struct {
	Sample   sauce.Sample
	Observed time.Time
}
type Status struct {
	Connected bool    `json:"connected"`
	Capacity  int     `json:"capacity"`
	Active    int     `json:"active"`
	Fresh     int     `json:"fresh"`
	Runtime   *Health `json:"runtime,omitempty"`
}
type Coordinator struct {
	mu       sync.Mutex
	token    string
	limit    int
	worker   *websocket.Conn
	out      chan Frame
	capacity int
	leases   map[int64]Lease
	latest   map[int64]Snapshot
	pending  map[string]chan Frame
	health   *Health
	healthAt time.Time
}

func New(token string, limit int) *Coordinator {
	if limit < 1 {
		limit = 20
	}
	return &Coordinator{token: token, limit: limit, leases: map[int64]Lease{}, latest: map[int64]Snapshot{}, pending: map[string]chan Frame{}}
}
func (c *Coordinator) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{Connected: c.worker != nil && c.capacity > 0, Capacity: c.capacity, Active: len(c.leases)}
	if c.worker != nil && time.Since(c.healthAt) < 12*time.Second && c.health != nil {
		copy := *c.health
		s.Runtime = &copy
	}
	for _, p := range c.latest {
		if time.Since(p.Observed) < FreshFor {
			s.Fresh++
		}
	}
	return s
}
func (c *Coordinator) Latest(id int64) (Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.latest[id]
	return p, ok && c.worker != nil && time.Since(p.Observed) < FreshFor
}
func (c *Coordinator) sendLocked(f Frame) bool {
	if c.worker == nil {
		return false
	}
	select {
	case c.out <- f:
		return true
	default:
		c.worker.CloseNow()
		return false
	}
}
func (c *Coordinator) planLocked() {
	leases := make([]Lease, 0, len(c.leases))
	for _, l := range c.leases {
		leases = append(leases, l)
	}
	sort.Slice(leases, func(i, j int) bool { return leases[i].AthleteID < leases[j].AthleteID })
	c.sendLocked(Frame{Type: "plan", Leases: leases})
}
func (c *Coordinator) Acquire(id int64) (func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.worker == nil || c.capacity == 0 {
		return nil, ErrUnavailable
	}
	if _, ok := c.leases[id]; ok {
		return nil, ErrCapacity
	}
	if len(c.leases) >= c.capacity {
		return nil, ErrCapacity
	}
	l := Lease{AthleteID: id, ID: rand.Text()}
	c.leases[id] = l
	c.planLocked()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.leases[id].ID == l.ID {
			delete(c.leases, id)
			delete(c.latest, id)
			c.planLocked()
		}
	}, nil
}
func (c *Coordinator) request(ctx context.Context, f Frame) ([]Profile, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	f.ID = rand.Text()
	response := make(chan Frame, 1)
	c.mu.Lock()
	if len(c.pending) >= 16 || c.worker == nil {
		c.mu.Unlock()
		return nil, ErrUnavailable
	}
	c.pending[f.ID] = response
	sent := c.sendLocked(f)
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, f.ID); c.mu.Unlock() }()
	if !sent {
		return nil, ErrUnavailable
	}
	select {
	case <-ctx.Done():
		return nil, ErrUnavailable
	case r := <-response:
		if r.Error != "" {
			return nil, ErrUnavailable
		}
		return r.Profiles, nil
	}
}
func (c *Coordinator) Search(ctx context.Context, query string) ([]Profile, error) {
	return c.request(ctx, Frame{Type: "search", Query: query})
}
func (c *Coordinator) Profile(ctx context.Context, id int64) (Profile, error) {
	list, err := c.request(ctx, Frame{Type: "profile", AthleteID: id})
	if err != nil {
		return Profile{}, err
	}
	if len(list) != 1 || list[0].ID != id {
		return Profile{}, errors.New("Der Zwift-Fahrer wurde nicht gefunden.")
	}
	return list[0], nil
}

// acceptPosition is always called under mu. Cached or replayed worldTime values
// cannot renew a rider's freshness, even after a reconnect of the collector.
func (c *Coordinator) acceptPosition(p *Position, now time.Time) {
	if p == nil {
		return
	}
	id := p.Athlete.ID
	l, ok := c.leases[id]
	if !ok || l.ID != p.Lease {
		return
	}
	s, err := sauce.NormalizeAthlete(p.Athlete, id)
	if err != nil {
		return
	}
	observed := time.UnixMilli(p.ObservedAt)
	if observed.After(now.Add(time.Second)) || now.Sub(observed) > FreshFor {
		return
	}
	// Age accounts for buffering inside Sauce; ObservedAt accounts for transit.
	observed = observed.Add(-time.Duration(*p.Athlete.Age * float64(time.Millisecond)))
	if observed.After(now) {
		observed = now
	}
	if now.Sub(observed) >= FreshFor {
		return
	}
	if previous, ok := c.latest[id]; ok && previous.Sample.WorldTime >= s.WorldTime {
		return
	}
	c.latest[id] = Snapshot{Sample: s, Observed: observed}
}

func (c *Coordinator) Handler(w http.ResponseWriter, r *http.Request) {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(c.token) < 32 || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(provided), []byte(c.token)) != 1 {
		http.Error(w, "unauthorized observer", 401)
		return
	}
	c.mu.Lock()
	if c.worker != nil {
		c.mu.Unlock()
		http.Error(w, "observer already connected", 409)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		c.mu.Unlock()
		return
	}
	out := make(chan Frame, 64)
	c.worker, c.out, c.capacity = conn, out, 0
	c.mu.Unlock()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer conn.CloseNow()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.worker == conn {
			c.worker, c.out, c.capacity = nil, nil, 0
			c.health = nil
			// Preserve worldTime watermarks while invalidating freshness.
			for id, p := range c.latest {
				p.Observed = time.Time{}
				c.latest[id] = p
			}
			for _, waiter := range c.pending {
				select {
				case waiter <- Frame{Error: "disconnected"}:
				default:
				}
			}
		}
	}()
	conn.SetReadLimit(64 * 1024)
	var hello Frame
	deadline, stop := context.WithTimeout(ctx, 5*time.Second)
	err = wsjson.Read(deadline, conn, &hello)
	stop()
	if err != nil || hello.Type != "hello" || hello.Capacity < 1 || hello.Capacity > 1000 {
		return
	}
	c.mu.Lock()
	c.capacity = min(hello.Capacity, c.limit)
	c.planLocked()
	c.mu.Unlock()
	go func() {
		defer cancel()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-out:
				d, stop := context.WithTimeout(ctx, 3*time.Second)
				err := wsjson.Write(d, conn, f)
				stop()
				if err != nil {
					return
				}
			case <-ticker.C:
				d, stop := context.WithTimeout(ctx, 3*time.Second)
				err := conn.Ping(d)
				stop()
				if err != nil {
					return
				}
			}
		}
	}()
	for {
		var f Frame
		if wsjson.Read(ctx, conn, &f) != nil {
			return
		}
		c.mu.Lock()
		switch f.Type {
		case "health":
			if f.Health != nil {
				c.health, c.healthAt = f.Health, time.Now()
			}
		case "position":
			c.acceptPosition(f.Position, time.Now())
		case "result":
			if len(f.Profiles) <= 25 {
				if pending := c.pending[f.ID]; pending != nil {
					select {
					case pending <- f:
					default:
					}
				}
			}
		}
		c.mu.Unlock()
	}
}

func (c *Coordinator) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.worker != nil {
		c.worker.CloseNow()
	}
}
