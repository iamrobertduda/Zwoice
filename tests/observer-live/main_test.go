package main

import (
	"github.com/iamrobertduda/Zwoice/internal/observer"
	"github.com/iamrobertduda/Zwoice/internal/proximity"
	"github.com/iamrobertduda/Zwoice/internal/sauce"
	"testing"
	"time"
)

func TestDistributionDoesNotCountRasterCornersAsSeparateGroups(t *testing.T) {
	positions := []proximity.Position{{Course: 1, X: 299, Y: 299}, {Course: 1, X: 301, Y: 299}, {Course: 1, X: 299, Y: 301}, {Course: 1, X: 301, Y: 301}}
	if separatedGroups(positions) != 1 {
		t.Fatal("nearby riders falsely qualified as distributed")
	}
	positions = append(positions, proximity.Position{Course: 2}, proximity.Position{Course: 1, X: 1000}, proximity.Position{Course: 1, Event: 5})
	if separatedGroups(positions) != 4 {
		t.Fatal("worlds, events or geographically separated riders not counted")
	}
}

// Test the measurement accounting, not Sauce coverage. In particular, a probe
// that stops getting scheduled must not qualify a server from a few good ticks.
func TestCapacityAccountingRejectsBlindIntervals(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		keepSample             func(int) bool
		keepRuntime            func(int) bool
		duplicate, interrupted bool
		duration               time.Duration
		want                   bool
	}{
		{name: "healthy", duration: 30 * time.Minute, want: true},
		{name: "every_other_sample_lost", duration: 30 * time.Minute, keepSample: func(i int) bool { return i%2 == 0 }},
		{name: "duplicates_cannot_replace_missing_intervals", duration: 30 * time.Minute, keepSample: func(i int) bool { return i%2 == 0 }, duplicate: true},
		{name: "probe_paused_for_almost_entire_run", duration: 30 * time.Minute, keepSample: func(i int) bool { return i > 7180 }},
		{name: "short_but_long_measurement_pause", duration: 30 * time.Minute, keepSample: func(i int) bool { return i < 10 || i > 18 }},
		{name: "missing_cpu_rss_intervals", duration: 30 * time.Minute, keepRuntime: func(i int) bool { return i%2 == 0 }},
		{name: "short_run", duration: time.Minute},
		{name: "interrupted", duration: 30 * time.Minute, interrupted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(1800000000, 0)
			r := report{started: start, lastSample: start, worlds: map[int]bool{}}
			for i := range 10 {
				r.Riders = append(r.Riders, &riderReport{AthleteID: int64(i + 1), lastFresh: start})
			}
			steps := int(tc.duration / sampleInterval)
			for i := 1; i <= steps; i++ {
				now := start.Add(time.Duration(i) * sampleInterval)
				latest := func(id int64) (observer.Snapshot, bool) {
					return observer.Snapshot{Observed: now.Add(-100 * time.Millisecond), Sample: sauce.Sample{AthleteID: id, Position: proximity.Position{Course: 1 + int(id%2), X: float64(id) * 400}}}, true
				}
				if tc.keepSample == nil || tc.keepSample(i) {
					r.observe(now, latest)
					if tc.duplicate {
						r.observe(now.Add(time.Millisecond), latest)
					}
				}
				if i%20 == 0 && (tc.keepRuntime == nil || tc.keepRuntime(i/20)) {
					r.observeRuntime(now, &observer.Health{Processes: 10, Responding: 10, CPUSamples: 10, RSSBytes: 100000000, CPUPercent: 30})
				}
			}
			r.finish(start.Add(tc.duration), tc.interrupted)
			if r.Qualified != tc.want {
				t.Fatalf("qualified=%v; coverage=%f missing=%d gap=%d telemetry=%d/%d", r.Qualified, r.SamplingCoverage, r.MissingSamples, r.MaxSamplingGapMS, r.CompleteRuntimeSamples, r.ExpectedRuntimeSamples)
			}
			if r.SamplingCoverage > 1 || r.Riders[0].FreshRatio > 1 {
				t.Fatal("duplicate observations inflated coverage")
			}
			if tc.name == "every_other_sample_lost" && (r.MissingSamples != 3600 || r.Riders[0].FreshRatio != 0.5) {
				t.Fatal("missing observations did not reduce freshness", r.MissingSamples, r.Riders[0].FreshRatio)
			}
		})
	}
}
