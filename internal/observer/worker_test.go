package observer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolSlotReuseAndDisconnect(t *testing.T) {
	var active, peak atomic.Int32
	p := &pool{cfg: WorkerConfig{Slots: make([]Credentials, 2)}, positions: make(chan Frame, 8)}
	started := make(chan *slot, 8)
	p.runner = func(ctx context.Context, s *slot) {
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- s
		<-ctx.Done()
		time.Sleep(10 * time.Millisecond) // child exit is not instantaneous
		active.Add(-1)
	}
	ctx := context.Background()
	if err := p.reconcile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	idle := <-started
	if err := p.reconcile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if p.slots[0] != idle {
		t.Fatal("empty plans restarted the lookup process")
	}
	a := Lease{AthleteID: 1, ID: "a"}
	b := Lease{AthleteID: 2, ID: "b"}
	if err := p.reconcile(ctx, []Lease{a, b}); err != nil {
		t.Fatal(err)
	}
	<-started
	<-started
	if err := p.reconcile(ctx, []Lease{{AthleteID: 3, ID: "c"}, b}); err != nil {
		t.Fatal(err)
	}
	<-started
	if peak.Load() > 2 {
		t.Fatal("slot reused before previous child exited")
	}
	if err := p.reconcile(ctx, []Lease{{AthleteID: 3, ID: "c"}, {AthleteID: 4, ID: "c"}}); err == nil {
		t.Fatal("duplicate leases accepted")
	}
	p.positions <- Frame{Type: "position"}
	p.stop()
	if active.Load() != 0 || len(p.positions) != 0 || len(p.slots) != 0 {
		t.Fatal("disconnect leaked processes or stale positions")
	}
}

func TestLookupForcesRefreshAndHasTotalDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/rpc/v1/getAthlete":
			var args []json.RawMessage
			json.NewDecoder(r.Body).Decode(&args)
			if len(args) != 2 || string(args[0]) != "42" {
				t.Error("wrong rider lookup")
			}
			var opts map[string]bool
			json.Unmarshal(args[1], &opts)
			if !opts["refresh"] || !opts["allowFetch"] {
				t.Error("ownership check could use a cached profile")
			}
			w.Write([]byte(`{"success":true,"value":{"id":42,"firstName":"Test","lastName":"Rider"}}`))
		case "/api/rpc/v1/searchAthletes":
			io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
		default:
			t.Error("wrong path")
		}
	}))
	defer server.Close()
	p := &pool{slots: []*slot{{base: server.URL}, {base: server.URL}}}
	profiles, err := p.lookup(context.Background(), Frame{Type: "profile", AthleteID: 42})
	if err != nil || len(profiles) != 1 || profiles[0].Name != "Test Rider" {
		t.Fatalf("lookup: %v %v", profiles, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := p.lookup(ctx, Frame{Type: "search", Query: "Test"}); err == nil {
		t.Fatal("timeout ignored")
	}
	if time.Since(start) > time.Second {
		t.Fatal("total deadline multiplied by number of processes")
	}
}

func TestRuntimeMetricUnits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"value":[{"cpu":{"percentCPUUsage":0.125},"memory":{"workingSetSize":1024}}]}`))
	}))
	defer server.Close()
	h, err := runtimeHealth(context.Background(), server.URL)
	if err != nil || h.RSSBytes != 1048576 || h.CPUPercent != 12.5 || h.CPUSamples != 1 {
		t.Fatalf("wrong units: %+v %v", h, err)
	}
}

func TestConfigRejectsInsecureOrConflictingWorkers(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "runtime")
	init := filepath.Join(dir, "init.mjs")
	os.WriteFile(exe, nil, 0700)
	os.WriteFile(init, nil, 0600)
	good := WorkerConfig{Server: "https://example.test", Token: strings.Repeat("a", 32), Executable: exe, DataPath: dir, InitScript: init, Slots: []Credentials{{MainUsername: "main@example.test", MainPassword: "test", MonitorUsername: "monitor@example.test", MonitorPassword: "test"}}}
	path := filepath.Join(dir, "config.json")
	check := func(cfg WorkerConfig, valid bool) {
		t.Helper()
		b, _ := json.Marshal(cfg)
		os.WriteFile(path, b, 0600)
		_, err := LoadWorkerConfig(path)
		if (err == nil) != valid {
			t.Fatalf("configuration acceptance: %v", err)
		}
	}
	check(good, true)
	bad := good
	bad.Server = "http://remote.example"
	check(bad, false)
	bad = good
	bad.Slots = append(append([]Credentials{}, good.Slots...), good.Slots[0])
	check(bad, false)
	bad = good
	bad.Executable = filepath.Join(dir, "missing")
	check(bad, false)
	b, _ := json.Marshal(good)
	os.WriteFile(path, append(b, []byte(` {"extra":true}`)...), 0600)
	if _, err := LoadWorkerConfig(path); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
