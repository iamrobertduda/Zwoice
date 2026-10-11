package proximity

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func rider(id string, x float64, now time.Time) Rider {
	return Rider{ID: id, Cohort: "public", Name: id, Position: Position{X: x, Course: 1, Realm: 1, Sport: "CYCLING"}, Updated: now, Active: true}
}

func TestGain(t *testing.T) {
	for _, tc := range []struct{ d, want float64 }{{0, 1}, {10, 1}, {35, .5}, {60, 0}, {100, 0}, {math.NaN(), 0}} {
		if got := Gain(tc.d); got != tc.want {
			t.Errorf("gain(%v)=%v, want %v", tc.d, got, tc.want)
		}
	}
}

func TestHysteresisAndIsolation(t *testing.T) {
	now := time.Now()
	riders := map[string]Rider{"a": rider("a", 0, now), "b": rider("b", 59, now)}
	peers, edges := Build(riders, nil, now)
	if len(peers["a"]) != 1 {
		t.Fatal("near riders not joined")
	}
	b := riders["b"]
	b.Position.X = 70
	riders["b"] = b
	peers, edges = Build(riders, edges, now)
	if len(peers["a"]) != 1 || peers["a"][0].Gain != 0 {
		t.Fatal("silent warm link should remain")
	}
	if p, _ := Build(riders, nil, now); len(p["a"]) != 0 {
		t.Fatal("new link joined outside hear radius")
	}
	b.Position.X = 75
	riders["b"] = b
	if p, _ := Build(riders, edges, now); len(p["a"]) != 0 {
		t.Fatal("link not dropped at exit boundary")
	}
	for _, mutate := range []func(*Rider){func(r *Rider) { r.Position.Course = 2 }, func(r *Rider) { r.Position.Event = 8 }, func(r *Rider) { r.Position.Realm = 2 }, func(r *Rider) { r.Position.Sport = "RUNNING" }, func(r *Rider) { r.Cohort = "other" }, func(r *Rider) { r.Active = false }, func(r *Rider) { r.Updated = now.Add(-StaleAfter) }, func(r *Rider) { r.Position.Z = 80 }, func(r *Rider) { r.Position.X = math.Inf(1) }} {
		b = rider("b", 1, now)
		mutate(&b)
		riders["b"] = b
		if p, _ := Build(riders, nil, now); len(p["a"]) != 0 {
			t.Fatalf("isolated rider became audible: %+v", b)
		}
	}
}

func TestSymmetricCapAndCellBoundary(t *testing.T) {
	now := time.Now()
	riders := map[string]Rider{}
	for i := 0; i < 30; i++ {
		id := fmt.Sprint(i)
		riders[id] = rider(id, float64(i)-15, now)
	}
	peers, edges := Build(riders, nil, now)
	for id, list := range peers {
		if len(list) > MaxPeers {
			t.Fatal("degree exceeded")
		}
		for _, peer := range list {
			found := false
			for _, reverse := range peers[peer.ID] {
				if reverse.ID == id {
					found = true
				}
			}
			if !found {
				t.Fatal("asymmetric edge")
			}
		}
	}
	peers2, _ := Build(riders, nil, now)
	if fmt.Sprint(peers) != fmt.Sprint(peers2) {
		t.Fatal("non-deterministic graph")
	}
	if len(edges) == 0 || len(peers["14"]) == 0 {
		t.Fatal("negative coordinate boundary lost neighbours")
	}
}

func BenchmarkThousandRiders(b *testing.B) {
	now := time.Now()
	riders := make(map[string]Rider)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprint(i)
		r := rider(id, float64(i%20)*12, now)
		r.Position.Y = float64(i/20) * 20
		riders[id] = r
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Build(riders, nil, now)
	}
}
