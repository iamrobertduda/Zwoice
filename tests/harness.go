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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"github.com/iamrobertduda/Zwoice/internal/broker"
	"github.com/iamrobertduda/Zwoice/internal/desktop"
	"github.com/iamrobertduda/Zwoice/internal/observer"
	"github.com/iamrobertduda/Zwoice/internal/sauce"
	"github.com/iamrobertduda/Zwoice/internal/testsupport"
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
	proof     string
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
	return map[string]any{"athlete": map[string]string{"name": name + " " + s.proof}, "version": 2, "athleteId": s.id, "self": true, "age": 0, "state": map[string]any{"x": s.x * 100, "y": 0, "z": 1000, "courseId": s.course, "eventSubgroupId": s.event, "worldTime": s.worldTime, "sport": "cycling"}}
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

	sources := map[string]*source{"1001": {id: 1001, course: 1, riding: true}, "1002": {id: 1002, x: 8, course: 1, riding: true}}
	observerToken := strings.Repeat("synthetic-observer-", 3)
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
		h := broker.New(broker.Config{Accounts: store, Observer: observer.New(observerToken, 20), CloudflareKeyID: os.Getenv("ZWOICE_TEST_CF_KEY"), CloudflareAPIToken: os.Getenv("ZWOICE_TEST_CF_TOKEN")})
		current.Store(h)
		go h.Run(ctx)
		go testsupport.Observe(ctx, serverURL, observerToken, 20, func(id int64) (sauce.Athlete, bool) {
			source := sources[strconv.FormatInt(id, 10)]
			if source == nil {
				return sauce.Athlete{}, false
			}
			data, _ := json.Marshal(source.snapshot())
			var a sauce.Athlete
			json.Unmarshal(data, &a)
			return a, true
		}, func(f observer.Frame) []observer.Profile {
			var results []observer.Profile
			for id, source := range sources {
				name := "Flo Weber"
				if id == "1002" {
					name = "Emma Klein"
				}
				source.mu.Lock()
				name += " " + source.proof
				source.mu.Unlock()
				if (f.Type == "profile" && source.id == f.AthleteID) || (f.Type == "search" && (id == f.Query || strings.Contains(strings.ToLower(name), strings.ToLower(f.Query)))) {
					results = append(results, observer.Profile{ID: source.id, Name: strings.TrimSpace(name)})
				}
			}
			return results
		})
	}
	reset()
	serve("127.0.0.1:18089", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { current.Load().Handler().ServeHTTP(w, r) }))

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
	mux.HandleFunc("POST /profile", func(w http.ResponseWriter, r *http.Request) {
		source := sources[r.URL.Query().Get("id")]
		if source == nil {
			http.Error(w, "unknown rider", 400)
			return
		}
		source.mu.Lock()
		source.proof = r.URL.Query().Get("code")
		source.mu.Unlock()
		w.WriteHeader(204)
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
