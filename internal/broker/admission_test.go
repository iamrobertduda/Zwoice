package broker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestPublicJoinAndDuplicate(t *testing.T) {
	hub := New(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	server := httptest.NewServer(hub.Handler())
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/session"
	opts := &websocket.DialOptions{HTTPHeader: http.Header{"Zwoice-Athlete-Id": []string{"123"}}}
	c, _, err := websocket.Dial(ctx, url, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	var welcome struct{ Type, ID string }
	if err := wsjson.Read(ctx, c, &welcome); err != nil || welcome.Type != "welcome" || welcome.ID != "123" {
		t.Fatalf("public admission failed: %+v %v", welcome, err)
	}
	duplicate, res, err := websocket.Dial(ctx, url, opts)
	if duplicate != nil {
		duplicate.CloseNow()
	}
	if err == nil || res.StatusCode != 409 {
		t.Fatal("duplicate accepted")
	}
	for _, id := range []string{"0", "-1", "abc", "9007199254740992"} {
		opts.HTTPHeader.Set("Zwoice-Athlete-ID", id)
		conn, res, err := websocket.Dial(ctx, url, opts)
		if conn != nil {
			conn.CloseNow()
		}
		if err == nil || res.StatusCode != 401 {
			t.Fatal("invalid ID accepted", id)
		}
	}
}

func TestPublicAdmissionResourceBounds(t *testing.T) {
	h := New(Config{})
	now := time.Now()
	for i := 0; i < 8; i++ {
		if code := h.reservePublic("one", fmt.Sprint(i), now); code != 0 {
			t.Fatal(code)
		}
	}
	if h.reservePublic("one", "extra", now) != http.StatusTooManyRequests {
		t.Fatal("per-IP capacity bypass")
	}
	if h.reservePublic("two", "0", now) != http.StatusConflict {
		t.Fatal("pending duplicate bypass")
	}
	for i := 0; i < 8; i++ {
		h.releasePublic("one", fmt.Sprint(i), true)
	}
	for i := 0; i < 12; i++ {
		if h.reservePublic("one", "next", now) != 0 {
			t.Fatal("lost capacity")
		}
		h.releasePublic("one", "next", true)
	}
	if h.reservePublic("one", "limited", now) != 429 {
		t.Fatal("burst rate bypass")
	}
	if h.reservePublic("one", "limited", now.Add(10*time.Second)) != 0 {
		t.Fatal("rate did not recover")
	}
	h.releasePublic("one", "limited", true)
	h.reservePublic("new", "clean", now.Add(11*time.Minute))
	if _, ok := h.admissions["one"]; ok {
		t.Fatal("expired IP retained")
	}
	for len(h.admissions) < 10000 {
		h.admissions[fmt.Sprint(len(h.admissions))] = &admission{updated: now.Add(11 * time.Minute)}
	}
	if h.reservePublic("overflow", "overflow", now.Add(11*time.Minute)) != 429 {
		t.Fatal("unbounded IP map")
	}
}
func TestProxyTrustAndIPv6Aggregation(t *testing.T) {
	r := httptest.NewRequest("GET", "http://server/v1/session", nil)
	r.RemoteAddr = "[2001:db8:1::1]:5678"
	r.Header.Set("X-Real-IP", "1.2.3.4")
	h := New(Config{})
	if h.admissionIP(r) != "2001:db8:1::/64" {
		t.Fatal("trusted unconfigured forwarding header")
	}
	h.cfg.TrustRailwayProxy = true
	if h.admissionIP(r) != "1.2.3.4" {
		t.Fatal("ignored Railway client IP")
	}
}
