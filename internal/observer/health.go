package observer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Responding means Sauce's authenticated startup reached its local API. It does
// not establish that a rider is visible; Fresh in Status is the coverage metric.
// RSS/CPU cover responding Sauce processes only, not the OS or Go worker.
type Health struct {
	Processes  int     `json:"processes"`
	Responding int     `json:"responding"`
	Restarts   uint64  `json:"restarts"`
	RSSBytes   uint64  `json:"rssBytes"`
	CPUPercent float64 `json:"cpuPercent"`
	CPUSamples int     `json:"cpuSamples"`
}

func (p *pool) health(ctx context.Context) Health {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	p.mu.Lock()
	slots := append([]*slot(nil), p.slots...)
	p.mu.Unlock()
	var result Health
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, s := range slots {
		result.Restarts += s.restarts.Load()
		if s.running.Load() {
			result.Processes++
		}
	}
	for _, s := range slots {
		if !s.running.Load() {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			break
		}
		wg.Go(func() {
			defer func() { <-sem }()
			h, err := runtimeHealth(ctx, s.base)
			if err != nil {
				return
			}
			mu.Lock()
			result.Responding++
			result.RSSBytes += h.RSSBytes
			result.CPUPercent += h.CPUPercent
			result.CPUSamples += h.CPUSamples
			mu.Unlock()
		})
	}
	wg.Wait()
	return result
}

func runtimeHealth(ctx context.Context, base string) (Health, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/rpc/v1/pollMetrics", strings.NewReader("[]"))
	if err != nil {
		return Health{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := sauceHTTP.Do(req)
	if err != nil {
		return Health{}, err
	}
	defer res.Body.Close()
	var envelope struct {
		Success bool `json:"success"`
		Value   []struct {
			CPU struct {
				Fraction *float64 `json:"percentCPUUsage"`
			} `json:"cpu"`
			Memory struct {
				KiB float64 `json:"workingSetSize"`
			} `json:"memory"`
		} `json:"value"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 16*1024)).Decode(&envelope) != nil || res.StatusCode != 200 || !envelope.Success || len(envelope.Value) != 1 {
		return Health{}, ErrUnavailable
	}
	metric := envelope.Value[0]
	if metric.Memory.KiB <= 0 || metric.Memory.KiB > 1<<40 {
		return Health{}, ErrUnavailable
	}
	h := Health{RSSBytes: uint64(metric.Memory.KiB * 1024)}
	if metric.CPU.Fraction != nil && *metric.CPU.Fraction >= 0 && *metric.CPU.Fraction < 10000 {
		h.CPUPercent = *metric.CPU.Fraction * 100
		h.CPUSamples = 1
	}
	return h, nil
}
