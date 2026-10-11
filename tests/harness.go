// Local-only integration fixture. Never included in the application binary.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/auth"
	"github.com/iamrobertduda/Zwoice/internal/broker"
	"github.com/iamrobertduda/Zwoice/internal/desktop"
)

type source struct {
	mu        sync.Mutex
	id        int64
	x         float64
	course    int
	frozen    bool
	worldTime int64
}

func (s *source) snapshot() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.frozen {
		s.worldTime = time.Now().UnixMilli()
	}
	return map[string]any{"version": 2, "athleteId": s.id, "self": true, "age": 0, "state": map[string]any{"x": s.x * 100, "y": 0, "z": 1000, "courseId": s.course, "eventSubgroupId": 0, "worldTime": s.worldTime, "sport": "cycling"}}
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
func main() {
	key := []byte(strings.Repeat("integration-test-only-", 3))
	serverURL := "http://127.0.0.1:18089"
	cohort := "test"
	if hosted := os.Getenv("ZWOICE_TEST_SERVER"); hosted != "" {
		key = []byte(os.Getenv("ZWOICE_TEST_SIGNING_KEY"))
		if len(key) < 32 {
			log.Fatal("Hosted test requires a signing key")
		}
		serverURL = hosted
		cohort = fmt.Sprintf("validation-%d", time.Now().UnixNano())
	}
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
		h := broker.New(broker.Config{Key: key})
		current.Store(h)
		go h.Run(ctx)
	}
	reset()
	serve("127.0.0.1:18089", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { current.Load().Handler().ServeHTTP(w, r) }))
	sources := map[string]*source{"1001": {id: 1001, course: 1}, "1002": {id: 1002, x: 8, course: 1}}
	serve("127.0.0.1:11081", sources["1001"].handler())
	serve("127.0.0.1:11082", sources["1002"].handler())
	serve("127.0.0.1:18787", desktop.Handler("127.0.0.1:18787"))
	serve("127.0.0.1:18788", desktop.Handler("127.0.0.1:18788"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		token, _ := auth.Sign(key, auth.Claims{AthleteID: id, Cohort: cohort, Expires: time.Now().Add(10 * time.Minute).Unix()})
		json.NewEncoder(w).Encode(map[string]any{"server": serverURL, "token": token})
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
		if v := r.URL.Query().Get("frozen"); v != "" {
			s.frozen = v == "true"
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, r *http.Request) { reset(); w.WriteHeader(204) })
	fmt.Println("Zwoice integration fixture ready")
	log.Fatal(http.ListenAndServe("127.0.0.1:11080", mux))
}
