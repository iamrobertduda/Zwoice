// Synthetic local signaling soak: includes server AND clients in one process.
package main

import (
	"context"
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
	"github.com/iamrobertduda/Zwoice/internal/proximity"
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
	h := broker.New(broker.Config{Accounts: store, MaxClients: *count, TrustRailwayProxy: true})
	go h.Run(ctx)
	server := httptest.NewServer(h.Handler())
	defer server.Close()
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
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/session", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}, "Zwoice-Athlete-ID": []string{fmt.Sprint(id)}, "X-Real-IP": []string{fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256)}}})
		if err != nil {
			log.Fatal(err)
		}
		connections = append(connections, conn)
		wg.Add(2)
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
		pos := proximity.Position{X: float64(i/4)*300 + float64(i%4)*8, Course: 1, Realm: 1, Sport: "CYCLING"}
		wsjson.Write(ctx, conn, broker.Message{Type: "voice", Active: true})
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if wsjson.Write(ctx, conn, broker.Message{Type: "position", Position: &pos}) != nil {
						return
					}
				}
			}
		}()
	}
	fmt.Printf("Connected %d clients; soak %s\n", *count, *duration)
	timer := time.NewTimer(*duration)
	<-timer.C
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
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
