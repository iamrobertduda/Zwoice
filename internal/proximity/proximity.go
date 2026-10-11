// Package proximity builds a symmetric, degree-bounded voice graph in metres.
package proximity

import (
	"math"
	"sort"
	"time"
)

const (
	FullRadius       = 10.0
	HearRadius       = 60.0
	LeaveRadius      = 75.0
	MaxPeers         = 6
	MaxCellOccupancy = 64
	StaleAfter       = 5 * time.Second
)

type Position struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Z      float64 `json:"z"`
	Course int     `json:"course"`
	Realm  int     `json:"realm"`
	Event  int64   `json:"event"`
	Sport  string  `json:"sport"`
}

func (p Position) Valid() bool {
	return p.Course > 0 && p.Course <= 100000 && p.Realm >= 0 && p.Event >= 0 && (p.Sport == "CYCLING" || p.Sport == "RUNNING") && finite(p.X) && finite(p.Y) && finite(p.Z)
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) < 1e7 }

func Gain(distance float64) float64 {
	if distance <= FullRadius {
		return 1
	}
	if distance >= HearRadius || math.IsNaN(distance) {
		return 0
	}
	t := (distance - FullRadius) / (HearRadius - FullRadius)
	return 1 - t*t*(3-2*t)
}

type Rider struct {
	ID       string
	Cohort   string
	Name     string
	Position Position
	Updated  time.Time
	Active   bool
}

type Peer struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Distance float64 `json:"distance"`
	Gain     float64 `json:"gain"`
}

type Edge struct{ A, B string }

// Near is also checked when routing signals, so a position change cannot use
// an edge retained from the previous graph tick to cross a world/event boundary.
func Near(a, b Rider, now time.Time, radius float64) bool {
	if !a.Active || !b.Active || !a.Position.Valid() || !b.Position.Valid() || now.Sub(a.Updated) >= StaleAfter || now.Sub(b.Updated) >= StaleAfter {
		return false
	}
	p, q := a.Position, b.Position
	return a.Cohort == b.Cohort && p.Course == q.Course && p.Realm == q.Realm && p.Event == q.Event && p.Sport == q.Sport && math.Pow(p.X-q.X, 2)+math.Pow(p.Y-q.Y, 2)+math.Pow(p.Z-q.Z, 2) < radius*radius
}

type Cell struct {
	cohort        string
	course, realm int
	event         int64
	sport         string
	x, y, z       int
}

func CellFor(r Rider) Cell {
	p := r.Position
	return Cell{r.Cohort, p.Course, p.Realm, p.Event, p.Sport, int(math.Floor(p.X / LeaveRadius)), int(math.Floor(p.Y / LeaveRadius)), int(math.Floor(p.Z / LeaveRadius))}
}

// CapacityAvailable bounds worst-case dense-cell work and memory. Updating an
// existing rider in its own cell does not consume another slot.
func CapacityAvailable(riders map[string]Rider, incoming Rider, now time.Time) bool {
	want, count := CellFor(incoming), 0
	for id, r := range riders {
		if id != incoming.ID && r.Active && now.Sub(r.Updated) < StaleAfter && CellFor(r) == want {
			count++
		}
	}
	return count < MaxCellOccupancy
}

// Build uses a spatial hash; only neighbouring cells in the same world, event,
// sport and invitation cohort are examined. Old links get a small preference
// to avoid churn, and remain warm (silent) between 60 and 75 metres.
func Build(riders map[string]Rider, previous map[Edge]bool, now time.Time) (map[string][]Peer, map[Edge]bool) {
	grid := make(map[Cell][]Rider)
	peers := make(map[string][]Peer, len(riders))
	ids := make([]string, 0, len(riders))
	for id := range riders {
		ids = append(ids, id)
		peers[id] = []Peer{}
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := riders[id]
		if r.Active && r.Position.Valid() && !r.Updated.IsZero() && now.Sub(r.Updated) < StaleAfter {
			key := CellFor(r)
			if len(grid[key]) < MaxCellOccupancy {
				grid[key] = append(grid[key], r)
			}
		}
	}
	type candidate struct {
		edge            Edge
		distance, score float64
	}
	var candidates []candidate
	for key, group := range grid {
		for _, a := range group {
			for dx := -1; dx <= 1; dx++ {
				for dy := -1; dy <= 1; dy++ {
					for dz := -1; dz <= 1; dz++ {
						next := key
						next.x += dx
						next.y += dy
						next.z += dz
						for _, b := range grid[next] {
							if a.ID >= b.ID {
								continue
							}
							edge := Edge{a.ID, b.ID}
							distance := math.Sqrt(math.Pow(a.Position.X-b.Position.X, 2) + math.Pow(a.Position.Y-b.Position.Y, 2) + math.Pow(a.Position.Z-b.Position.Z, 2))
							limit, bias := HearRadius, 0.0
							if previous[edge] {
								limit, bias = LeaveRadius, 8
							}
							if distance < limit {
								candidates = append(candidates, candidate{edge, distance, distance - bias})
							}
						}
					}
				}
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.score != b.score {
			return a.score < b.score
		}
		if a.edge.A != b.edge.A {
			return a.edge.A < b.edge.A
		}
		return a.edge.B < b.edge.B
	})
	edges := make(map[Edge]bool)
	for _, c := range candidates {
		a, b := c.edge.A, c.edge.B
		if len(peers[a]) >= MaxPeers || len(peers[b]) >= MaxPeers {
			continue
		}
		edges[c.edge] = true
		d := math.Round(c.distance*10) / 10
		peers[a] = append(peers[a], Peer{b, riders[b].Name, d, Gain(c.distance)})
		peers[b] = append(peers[b], Peer{a, riders[a].Name, d, Gain(c.distance)})
	}
	return peers, edges
}
