package broker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/observer"
)

func connectedObserver(t *testing.T) *observer.Coordinator {
	t.Helper()
	token := strings.Repeat("o", 32)
	c := observer.New(token, 20)
	srv := httptest.NewServer(http.HandlerFunc(c.Handler))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	if err := wsjson.Write(ctx, conn, observer.Frame{Type: "hello", Capacity: 20}); err != nil {
		t.Fatal(err)
	}
	var plan observer.Frame
	if err := wsjson.Read(ctx, conn, &plan); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			var f observer.Frame
			if wsjson.Read(ctx, conn, &f) != nil {
				return
			}
		}
	}()
	return c
}

func TestUnlinkedRiderCannotMintTURN(t *testing.T) {
	store, token, _ := accountFixture(t)
	if err := store.UnlinkZwift(token); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer provider.Close()
	h := New(Config{Accounts: store, Observer: connectedObserver(t), CloudflareKeyID: "test", CloudflareAPIToken: "test", TURNEndpoint: provider.URL})
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/session", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}, "Zwoice-Athlete-ID": []string{"123"}}})
	if c != nil {
		c.CloseNow()
	}
	if err == nil || res == nil || res.StatusCode != 412 || calls.Load() != 0 {
		t.Fatal("unlinked user bypassed rider/TURN gate", err, calls.Load())
	}
}
