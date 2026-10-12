// Synthetic local signaling soak: includes server AND clients in one process.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"github.com/iamrobertduda/Zwoice/internal/broker"
	"github.com/iamrobertduda/Zwoice/internal/observer"
	"github.com/iamrobertduda/Zwoice/internal/sauce"
	"github.com/iamrobertduda/Zwoice/internal/testsupport"
)

type mailbox struct{ code string }

func (m *mailbox) SendLogin(_ context.Context, _ string, code, _ string) error {
	m.code = code
	return nil
}
func main() {
	count := flag.Int("clients", 1000, "number of synthetic clients")
	duration := flag.Duration("duration", time.Minute, "steady-state duration")
	flag.Parse()
	dir, err := os.MkdirTemp("", "zwoice-load-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mail := &mailbox{}
	store, err := accounts.Open(accounts.Config{Path: filepath.Join(dir, "accounts.db"), Mailer: mail, DailyEmailLimit: 10000})
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observerToken := strings.Repeat("test-observer-", 3)
	observations := observer.New(observerToken, *count)
	h := broker.New(broker.Config{Accounts: store, Observer: observations, MaxClients: *count, TrustRailwayProxy: true})
	go h.Run(ctx)
	server := httptest.NewServer(h.Handler())
	defer server.Close()
	go testsupport.Observe(ctx, server.URL, observerToken, *count, func(id int64) (sauce.Athlete, bool) {
		i := int(id) - 1
		data, _ := json.Marshal(map[string]any{"version": 2, "athleteId": id, "age": 0, "state": map[string]any{"x": (float64(i/4)*300 + float64(i%4)*8) * 100, "y": 0, "z": 0, "courseId": 1 + i/20, "eventSubgroupId": i / 4, "sport": "cycling", "worldTime": time.Now().UnixMilli()}})
		var a sauce.Athlete
		json.Unmarshal(data, &a)
		return a, true
	}, nil)
	until := time.Now().Add(5 * time.Second)
	for !observations.Status().Connected && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	if !observations.Status().Connected {
		log.Fatal("synthetic observer did not connect")
	}
	baseline := runtime.NumGoroutine()
	var readCount atomic.Int64
	var failures atomic.Int64
	var wg sync.WaitGroup
	connections := make([]*websocket.Conn, 0, *count)
	for i := 0; i < *count; i++ {
		id := int64(i + 1)
		// Respect the production global admission budget during ramp-up.
		if i >= 50 {
			time.Sleep(510 * time.Millisecond)
		}
		challenge, err := store.RequestCode(ctx, fmt.Sprintf("load-%d@example.test", i))
		if err != nil {
			log.Fatal(err)
		}
		token, _, _, err := store.VerifyCode(challenge, mail.code, false)
		if err != nil {
			log.Fatal(err)
		}
		link, err := store.BeginZwiftLink(token, id)
		if err != nil {
			log.Fatal(err)
		}
		if err := store.ConfirmZwiftLink(token, link, id, "Synthetic rider "+link.Code); err != nil {
			log.Fatal(err)
		}
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/session", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}, "Zwoice-Athlete-ID": []string{fmt.Sprint(id)}, "X-Real-IP": []string{fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256)}}})
		if err != nil {
			log.Fatal(err)
		}
		connections = append(connections, conn)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				var value any
				if wsjson.Read(ctx, conn, &value) != nil {
					if ctx.Err() == nil {
						failures.Add(1)
					}
					return
				}
				readCount.Add(1)
			}
		}()
		wsjson.Write(ctx, conn, broker.Message{Type: "voice", Active: true})
	}
	fmt.Printf("Connected %d clients; soak %s\n", *count, *duration)
	timer := time.NewTimer(*duration)
	<-timer.C
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	fmt.Printf("Synthetic central positions: %d/%d fresh\n", observations.Status().Fresh, *count)
	fmt.Printf("Messages received: %d; unexpected disconnects: %d; combined server+load-client heap: %.1f MiB; goroutines: %d\n", readCount.Load(), failures.Load(), float64(mem.HeapAlloc)/1048576, runtime.NumGoroutine())
	cancel()
	for _, conn := range connections {
		conn.CloseNow()
	}
	wg.Wait()
	server.Close()
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	runtime.ReadMemStats(&mem)
	fmt.Printf("After cleanup: %d goroutines (baseline %d), heap %.1f MiB\n", runtime.NumGoroutine(), baseline, float64(mem.HeapAlloc)/1048576)
	if failures.Load() != 0 || runtime.NumGoroutine() > baseline+12 {
		log.Fatal("load validation failed")
	}
}
