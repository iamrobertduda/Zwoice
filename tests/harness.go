// Local-only integration fixture. Never included in the application binary.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"github.com/iamrobertduda/Zwoice/internal/broker"
	"github.com/iamrobertduda/Zwoice/internal/desktop"
)

type source struct {
	mu        sync.Mutex
	event     int64
	id        int64
	x         float64
	course    int
	riding    bool
	frozen    bool
	worldTime int64
}

func (s *source) snapshot() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.frozen {
		s.worldTime = time.Now().UnixMilli()
	}
	if !s.riding {
		return map[string]any{"version": 2, "athleteId": s.id, "self": true, "age": 0}
	}
	name := "Flo Weber"
	if s.id == 1002 {
		name = "Emma Klein"
	}
	return map[string]any{"athlete": map[string]string{"name": name}, "version": 2, "athleteId": s.id, "self": true, "age": 0, "state": map[string]any{"x": s.x * 100, "y": 0, "z": 1000, "courseId": s.course, "eventSubgroupId": s.event, "worldTime": s.worldTime, "sport": "cycling"}}
}
func (s *source) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/athlete/v2/self", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(s.snapshot()) })
	mux.HandleFunc("GET /api/ws/events", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		var request struct {
			Type string `json:"type"`
			UID  int    `json:"uid"`
			Data struct {
				Method string `json:"method"`
				Arg    struct {
					Event string `json:"event"`
					SubID int    `json:"subId"`
				} `json:"arg"`
			} `json:"data"`
		}
		if wsjson.Read(ctx, conn, &request) != nil {
			return
		}
		if request.Data.Method != "subscribe" || request.Data.Arg.Event != "athlete/self/v2" {
			return
		}
		wsjson.Write(ctx, conn, map[string]any{"type": "response", "uid": request.UID, "success": true})
		ctx = conn.CloseRead(ctx)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if wsjson.Write(ctx, conn, map[string]any{"type": "event", "uid": request.Data.Arg.SubID, "success": true, "data": s.snapshot()}) != nil {
					return
				}
			}
		}
	})
	return mux
}
func serve(address string, h http.Handler) {
	go func() { log.Fatal(http.ListenAndServe(address, h)) }()
}

type mailbox struct {
	mu    sync.Mutex
	codes map[string]string
}

func (m *mailbox) SendLogin(_ context.Context, email, code, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[email] = code
	return nil
}
func main() {
	directory, err := os.MkdirTemp("", "zwoice-browser-accounts-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(directory)
	mail := &mailbox{codes: map[string]string{}}
	store, err := accounts.Open(accounts.Config{Path: filepath.Join(directory, "accounts.db"), Mailer: mail, AdminEmail: "owner@example.test", DailyEmailLimit: 10000})
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	serverURL := "http://127.0.0.1:18089"

	var current atomic.Pointer[broker.Hub]
	var cancel context.CancelFunc
	var resetMu sync.Mutex
	reset := func() {
		resetMu.Lock()
		defer resetMu.Unlock()
		if cancel != nil {
			cancel()
		}
		ctx, stop := context.WithCancel(context.Background())
		cancel = stop
		h := broker.New(broker.Config{Accounts: store, CloudflareKeyID: os.Getenv("ZWOICE_TEST_CF_KEY"), CloudflareAPIToken: os.Getenv("ZWOICE_TEST_CF_TOKEN")})
		current.Store(h)
		go h.Run(ctx)
	}
	reset()
	serve("127.0.0.1:18089", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { current.Load().Handler().ServeHTTP(w, r) }))
	sources := map[string]*source{"1001": {id: 1001, course: 1, riding: true}, "1002": {id: 1002, x: 8, course: 1, riding: true}}

	serve("127.0.0.1:11081", sources["1001"].handler())
	serve("127.0.0.1:11082", sources["1002"].handler())
	serve("127.0.0.1:18787", desktop.Handler("127.0.0.1:18787"))
	serve("127.0.0.1:18788", desktop.Handler("127.0.0.1:18788"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mail", func(w http.ResponseWriter, r *http.Request) {
		mail.mu.Lock()
		defer mail.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"code": mail.codes[r.URL.Query().Get("email")]})
	})
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"server": serverURL})
	})
	mux.HandleFunc("POST /move", func(w http.ResponseWriter, r *http.Request) {
		s := sources[r.URL.Query().Get("id")]
		if s == nil {
			http.Error(w, "unknown rider", 400)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if v := r.URL.Query().Get("x"); v != "" {
			s.x, _ = strconv.ParseFloat(v, 64)
		}
		if v := r.URL.Query().Get("course"); v != "" {
			s.course, _ = strconv.Atoi(v)
		}
		if v := r.URL.Query().Get("riding"); v != "" {
			s.riding = v == "true"
		}
		if v := r.URL.Query().Get("frozen"); v != "" {
			s.frozen = v == "true"
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, r *http.Request) { reset(); w.WriteHeader(204) })
	fmt.Println("Zwoice integration fixture ready")
	log.Fatal(http.ListenAndServe("127.0.0.1:11080", mux))
}
