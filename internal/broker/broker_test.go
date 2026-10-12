package broker

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iamrobertduda/Zwoice/internal/auth"
	"github.com/iamrobertduda/Zwoice/internal/proximity"
)

func fakeClient(id string, now time.Time) *client {
	return &client{claims: auth.Claims{Expires: now.Add(time.Hour).Unix()}, rider: proximity.Rider{ID: id, Cohort: "test", Name: id, Active: true}, out: make(chan any, 32)}
}
func TestSignalAuthorizationAndFreshness(t *testing.T) {
	now := time.Now()
	h := New(Config{})
	a, b, c := fakeClient("a", now), fakeClient("b", now), fakeClient("c", now)
	h.clients = map[string]*client{"a": a, "b": b, "c": c}
	pos := proximity.Position{Course: 1, Realm: 1, Sport: "CYCLING"}
	for _, client := range h.clients {
		h.applyPosition(client, &pos, now)
	}
	c.rider.Cohort = "other"
	h.tick(now)
	for _, client := range h.clients {
		<-client.out
	}
	signal := Message{Type: "signal", To: "b", Payload: json.RawMessage(`{"candidate":{"candidate":"test"}}`)}
	h.handle(a, signal, now)
	if len(b.out) != 1 {
		t.Fatal("authorized signal missing")
	}
	<-b.out
	signal.To = "c"
	h.handle(a, signal, now)
	if len(c.out) != 0 {
		t.Fatal("signal crossed cohort")
	}
	signal.To = "b"
	b.rider.Position.Course = 2
	h.handle(a, signal, now)
	if len(b.out) != 0 {
		t.Fatal("old graph edge crossed world")
	}
	b.rider.Position.Course = 1
	b.rider.Updated = now.Add(-proximity.StaleAfter)
	h.handle(a, signal, now)
	if len(b.out) != 0 {
		t.Fatal("stale recipient received signal")
	}
	b.rider.Updated = now
	a.rider.Active = false
	h.handle(a, signal, now)
	if len(b.out) != 0 {
		t.Fatal("disabled sender received authorization")
	}
}

func TestCapacityRecoversAndIndexDoesNotLeak(t *testing.T) {
	h := New(Config{})
	now := time.Now()
	p := proximity.Position{Course: 1, Realm: 1, Sport: "CYCLING"}
	for i := 0; i < 65; i++ {
		id := fmt.Sprint(i)
		c := fakeClient(id, now)
		h.clients[id] = c
		h.applyPosition(c, &p, now)
	}
	if !h.clients["64"].rider.Updated.IsZero() {
		t.Fatal("dense cell admitted beyond capacity")
	}
	h.applyPosition(h.clients["0"], nil, now)
	h.applyPosition(h.clients["64"], &p, now.Add(time.Second))
	if h.clients["64"].rider.Updated.IsZero() {
		t.Fatal("capacity failed to recover")
	}
	for _, c := range h.clients {
		h.removeCell(c)
	}
	if len(h.cells) != 0 {
		t.Fatal("spatial index leaked")
	}
}

func TestHTTPAuthBoundary(t *testing.T) {
	h := New(Config{Key: []byte(strings.Repeat("a", 32))}).Handler()
	for _, tc := range []struct {
		origin string
		code   int
	}{{"", 401}, {"https://evil.example", 403}} {
		r := httptest.NewRequest("GET", "https://example.com/v1/session?token=unused", nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Errorf("got %d, expected %d", w.Code, tc.code)
		}
	}
}

func TestClientCannotPublishPositionOrIdentity(t *testing.T) {
	h := New(Config{})
	now := time.Now()
	c := fakeClient("123", now)
	p := proximity.Position{Course: 1, Realm: 1, Sport: "CYCLING"}
	h.handle(c, Message{Type: "position", Position: &p}, now)
	h.handle(c, Message{Type: "hello", Name: "Another rider"}, now)
	if !c.rider.Updated.IsZero() || c.rider.Name != "123" {
		t.Fatal("client overrode authoritative rider data")
	}
}
