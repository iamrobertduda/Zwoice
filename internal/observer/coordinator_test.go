package observer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/sauce"
)

func position(id, worldTime int64, age float64) sauce.Athlete {
	data, _ := json.Marshal(map[string]any{"version": 2, "athleteId": id, "self": false, "age": age, "state": map[string]any{"x": 100, "y": 0, "z": 0, "courseId": 1, "eventSubgroupId": 0, "sport": "cycling", "worldTime": worldTime}})
	var a sauce.Athlete
	_ = json.Unmarshal(data, &a)
	return a
}
func TestFreshnessLeaseAndReplay(t *testing.T) {
	c := New(strings.Repeat("x", 32), 20)
	c.leases[123] = Lease{AthleteID: 123, ID: "current"}
	now := time.Now()
	p := Position{Lease: "old", Athlete: position(123, 100, 0), ObservedAt: now.UnixMilli()}
	c.acceptPosition(&p, now)
	if len(c.latest) != 0 {
		t.Fatal("stale lease accepted")
	}
	p.Lease = "current"
	p.ObservedAt = now.Add(-4 * time.Second).UnixMilli()
	c.acceptPosition(&p, now)
	if len(c.latest) != 0 {
		t.Fatal("network delayed data accepted")
	}
	p.ObservedAt = now.Add(2 * time.Second).UnixMilli()
	c.acceptPosition(&p, now)
	if len(c.latest) != 0 {
		t.Fatal("future timestamp accepted")
	}
	p.ObservedAt = now.UnixMilli()
	p.Athlete = position(124, 100, 0)
	c.acceptPosition(&p, now)
	if len(c.latest) != 0 {
		t.Fatal("other rider accepted")
	}
	p.Athlete = position(123, 100, 0)
	c.acceptPosition(&p, now)
	first := c.latest[123]
	if first.Sample.WorldTime != 100 {
		t.Fatal("fresh explicit rider rejected")
	}
	p.ObservedAt = now.Add(time.Second).UnixMilli()
	c.acceptPosition(&p, now.Add(time.Second))
	if !c.latest[123].Observed.Equal(first.Observed) {
		t.Fatal("replay renewed freshness")
	}
	p.Athlete = position(123, 101, 2900)
	p.ObservedAt = now.Add(-time.Second).UnixMilli()
	c.acceptPosition(&p, now)
	if c.latest[123].Sample.WorldTime != 100 {
		t.Fatal("Sauce age and transit not combined")
	}
	p.Athlete = position(123, 102, 0)
	p.ObservedAt = now.UnixMilli()
	c.acceptPosition(&p, now)
	if c.latest[123].Sample.WorldTime != 102 {
		t.Fatal("new sample rejected")
	}
}

func TestTwentyRiderPoolCapacityAndDisconnect(t *testing.T) {
	token := strings.Repeat("s", 32)
	c := New(token, 20)
	srv := httptest.NewServer(http.HandlerFunc(c.Handler))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint := "ws" + strings.TrimPrefix(srv.URL, "http")
	bad, res, err := websocket.Dial(ctx, endpoint, nil)
	if bad != nil {
		bad.CloseNow()
	}
	if err == nil || res.StatusCode != 401 {
		t.Fatal("anonymous worker accepted")
	}
	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := wsjson.Write(ctx, conn, Frame{Type: "hello", Capacity: 100}); err != nil {
		t.Fatal(err)
	}
	var plan Frame
	if err := wsjson.Read(ctx, conn, &plan); err != nil {
		t.Fatal(err)
	}
	releases := []func(){}
	for id := int64(1); id <= 20; id++ {
		release, err := c.Acquire(id)
		if err != nil {
			t.Fatal(id, err)
		}
		releases = append(releases, release)
		if err := wsjson.Read(ctx, conn, &plan); err != nil {
			t.Fatal(err)
		}
	}
	if len(plan.Leases) != 20 || c.Status().Capacity != 20 {
		t.Fatal("capacity cap lost")
	}
	if _, err := c.Acquire(21); err != ErrCapacity {
		t.Fatal("over capacity admitted", err)
	}
	for _, l := range plan.Leases {
		f := Frame{Type: "position", Position: &Position{Lease: l.ID, Athlete: position(l.AthleteID, 100, 0), ObservedAt: time.Now().UnixMilli()}}
		if err := wsjson.Write(ctx, conn, f); err != nil {
			t.Fatal(err)
		}
	}
	until := time.Now().Add(time.Second)
	for c.Status().Fresh != 20 && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if c.Status().Fresh != 20 {
		t.Fatal("not all riders received current positions", c.Status())
	}
	conn.CloseNow()
	until = time.Now().Add(time.Second)
	for c.Status().Connected && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if _, ok := c.Latest(1); ok {
		t.Fatal("disconnect retained audible position")
	}
	for _, release := range releases {
		release()
	}
	if c.Status().Active != 0 {
		t.Fatal("lease leak")
	}
}
