// Real-data qualification tool. Never substitutes synthetic positions or
// connects to production: the collector runs against a temporary local broker.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/iamrobertduda/Zwoice/internal/observer"
	"github.com/iamrobertduda/Zwoice/internal/proximity"
)

type riderReport struct {
	AthleteID  int64   `json:"athleteId"`
	Samples    int     `json:"samples"`
	Fresh      int     `json:"fresh"`
	FreshRatio float64 `json:"freshRatio"`
	MaxGapMS   int64   `json:"maxGapMs"`
	P95AgeMS   int     `json:"p95AgeMs"`
	histogram  [31]int
	lastFresh  time.Time
}
type report struct {
	Started                time.Time      `json:"started"`
	Seconds                float64        `json:"seconds"`
	WarmupSeconds          float64        `json:"warmupSeconds"`
	Riders                 []*riderReport `json:"riders"`
	Worlds                 int            `json:"worlds"`
	PeakSeparatedGroups    int            `json:"peakSeparated300mGroups"`
	RuntimeSamples         int            `json:"runtimeSamples"`
	CompleteRuntimeSamples int            `json:"completeRuntimeSamples"`
	PeakSauceRSSBytes      uint64         `json:"peakSauceRssBytes"`
	PeakSauceCPUPercent    float64        `json:"peakSauceCpuPercent"`
	Qualified              bool           `json:"qualified"`
	DistributionSamples    int            `json:"distributionSamples"`
	TotalSamples           int            `json:"totalSamples"`
	Scope                  string         `json:"scope"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	config := flag.String("config", "", "operator-owned secret observer configuration")
	ids := flag.String("riders", "", "comma-separated consenting test riders' numeric IDs")
	duration := flag.Duration("duration", 30*time.Minute, "measured period, excluding warmup")
	warmup := flag.Duration("warmup", 2*time.Minute, "startup excluded from measurement")
	output := flag.String("output", "observer-report.json", "private JSON report (contains rider IDs)")
	flag.Parse()
	if _, err := os.Stat(*output); !os.IsNotExist(err) {
		return fmt.Errorf("report path already exists or cannot be inspected")
	}
	if *duration <= 0 || *warmup < 0 {
		return fmt.Errorf("invalid duration")
	}
	cfg, err := observer.LoadWorkerConfig(*config)
	if err != nil {
		return err
	}
	var riders []*riderReport
	seen := map[int64]bool{}
	for _, v := range strings.Split(*ids, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || id <= 0 || id > 9007199254740991 || seen[id] {
			return fmt.Errorf("provide distinct valid test rider IDs")
		}
		seen[id] = true
		riders = append(riders, &riderReport{AthleteID: id})
	}
	if len(riders) > len(cfg.Slots) {
		return fmt.Errorf("not enough configured observer slots")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	cfg.Token = rand.Text() + rand.Text()
	coordinator := observer.New(cfg.Token, len(riders))
	defer coordinator.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/observer", coordinator.Handler)
	server := httptest.NewServer(mux)
	defer server.Close()
	cfg.Server = server.URL
	workerDone := make(chan error, 1)
	go func() { workerDone <- observer.RunWorker(ctx, cfg) }()
	defer func() { cancel(); <-workerDone }()
	deadline := time.Now().Add(10 * time.Second)
	for !coordinator.Status().Connected {
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("worker did not connect")
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, r := range riders {
		release, err := coordinator.Acquire(r.AthleteID)
		if err != nil {
			return err
		}
		defer release()
	}
	fmt.Fprintf(os.Stderr, "Real observer test: %d riders; warmup %s; measurement %s. Interrupt saves a partial report.\n", len(riders), *warmup, *duration)
	if !wait(ctx, *warmup) {
		return fmt.Errorf("interrupted before measurement")
	}
	result := report{Started: time.Now().UTC(), WarmupSeconds: warmup.Seconds(), Riders: riders, Scope: "Real Sauce positions. No audio test. CPU/RSS cover responding Sauce processes only; OS and worker excluded. Qualification requires 10 or 20 riders, >=30 min, >=2 worlds, >=4 groups separated by 300m or world/event boundaries, >=99% fresh samples for every rider, no gap >3s, distribution present for >=95% of samples, and complete runtime telemetry >=95%. Licensing is a separate release gate."}
	for _, r := range riders {
		r.lastFresh = result.Started
	}
	worlds := map[int]bool{}
	sample := time.NewTicker(250 * time.Millisecond)
	defer sample.Stop()
	telemetry := time.NewTicker(5 * time.Second)
	defer telemetry.Stop()
	end := time.NewTimer(*duration)
	defer end.Stop()
	running := true
	for running {
		select {
		case <-ctx.Done():
			running = false
		case <-end.C:
			running = false
		case now := <-sample.C:
			var positions []proximity.Position
			sampleWorlds := map[int]bool{}
			result.TotalSamples++
			for _, r := range riders {
				r.Samples++
				p, fresh := coordinator.Latest(r.AthleteID)
				gap := now.Sub(r.lastFresh).Milliseconds()
				if gap > r.MaxGapMS {
					r.MaxGapMS = gap
				}
				if !fresh {
					continue
				}
				r.Fresh++
				r.lastFresh = now
				age := max(0, int(now.Sub(p.Observed).Milliseconds()))
				r.histogram[min(age/100, 30)]++
				pos := p.Sample.Position
				worlds[pos.Course] = true
				positions = append(positions, pos)
				sampleWorlds[pos.Course] = true
			}
			groups := separatedGroups(positions)
			result.PeakSeparatedGroups = max(result.PeakSeparatedGroups, groups)
			if len(sampleWorlds) >= 2 && groups >= 4 {
				result.DistributionSamples++
			}
		case <-telemetry.C:
			result.RuntimeSamples++
			if h := coordinator.Status().Runtime; h != nil {
				result.PeakSauceRSSBytes = max(result.PeakSauceRSSBytes, h.RSSBytes)
				result.PeakSauceCPUPercent = max(result.PeakSauceCPUPercent, h.CPUPercent)
				if h.Processes == len(riders) && h.Responding == len(riders) && h.CPUSamples == len(riders) {
					result.CompleteRuntimeSamples++
				}
			}
		}
	}
	result.Seconds = time.Since(result.Started).Seconds()
	result.Worlds = len(worlds)
	result.Qualified = ctx.Err() == nil && (len(riders) == 10 || len(riders) == 20) && result.Seconds >= 1800 && result.TotalSamples > 0 && float64(result.DistributionSamples)/float64(result.TotalSamples) >= 0.95 && result.RuntimeSamples > 0 && float64(result.CompleteRuntimeSamples)/float64(result.RuntimeSamples) >= 0.95
	for _, r := range riders {
		if r.Samples > 0 {
			r.FreshRatio = float64(r.Fresh) / float64(r.Samples)
		}
		total := 0
		for bucket, count := range r.histogram {
			total += count
			if r.Fresh > 0 && float64(total) >= float64(r.Fresh)*0.95 {
				r.P95AgeMS = (bucket + 1) * 100
				break
			}
		}
		r.MaxGapMS = max(r.MaxGapMS, time.Since(r.lastFresh).Milliseconds())
		result.Qualified = result.Qualified && r.FreshRatio >= 0.99 && r.MaxGapMS <= 3000
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Fprintf(os.Stderr, "Saved %s. Capacity qualification: %v\n", *output, result.Qualified)
	if !result.Qualified {
		return fmt.Errorf("capacity not qualified; inspect the saved measurements")
	}
	return nil
}

func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Conservative greedy count: retained representatives are mutually separated.
// Raster cells alone can falsely count four groups clustered on a cell corner.
func separatedGroups(positions []proximity.Position) int {
	var representatives []proximity.Position
	for _, p := range positions {
		separated := true
		for _, q := range representatives {
			if p.Course == q.Course && p.Event == q.Event && p.Sport == q.Sport && (p.X-q.X)*(p.X-q.X)+(p.Y-q.Y)*(p.Y-q.Y)+(p.Z-q.Z)*(p.Z-q.Z) < 300*300 {
				separated = false
				break
			}
		}
		if separated {
			representatives = append(representatives, p)
		}
	}
	return len(representatives)
}
