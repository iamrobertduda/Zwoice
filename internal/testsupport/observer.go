// Package testsupport supplies explicit synthetic sources for local tests only.
// It is not imported by cmd/zwoice or by any production package.
package testsupport

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/observer"
	"github.com/iamrobertduda/Zwoice/internal/sauce"
)

// Observe exercises the real private observer wire protocol, without claiming
// to exercise Zwift's network coverage or the CPU cost of Sauce itself.
func Observe(ctx context.Context, server, token string, capacity int, source func(int64) (sauce.Athlete, bool), profiles func(observer.Frame) []observer.Profile) {
	for ctx.Err() == nil {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server, "http")+"/internal/observer", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
		if err == nil {
			run(ctx, conn, capacity, source, profiles)
			conn.CloseNow()
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func run(parent context.Context, conn *websocket.Conn, capacity int, source func(int64) (sauce.Athlete, bool), profiles func(observer.Frame) []observer.Profile) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if wsjson.Write(ctx, conn, observer.Frame{Type: "hello", Capacity: capacity}) != nil {
		return
	}
	var mu sync.Mutex
	var leases []observer.Lease
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				plan := append([]observer.Lease{}, leases...)
				mu.Unlock()
				for _, lease := range plan {
					a, ok := source(lease.AthleteID)
					if !ok {
						continue
					}
					f := observer.Frame{Type: "position", Position: &observer.Position{Lease: lease.ID, Athlete: a, ObservedAt: time.Now().UnixMilli()}}
					if wsjson.Write(ctx, conn, f) != nil {
						return
					}
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	for {
		var f observer.Frame
		if wsjson.Read(ctx, conn, &f) != nil {
			return
		}
		if f.Type == "plan" {
			mu.Lock()
			leases = f.Leases
			mu.Unlock()
		}
		if f.Type == "search" || f.Type == "profile" {
			var result []observer.Profile
			if profiles != nil {
				result = profiles(f)
			}
			if wsjson.Write(ctx, conn, observer.Frame{Type: "result", ID: f.ID, Profiles: result}) != nil {
				return
			}
		}
	}
}
