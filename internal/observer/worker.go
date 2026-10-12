package observer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/sauce"
)

// Credentials belong to the operator, never to a Zwoice end user. Supply this
// configuration as a read-only secret file; it is never sent to the broker.
type WorkerConfig struct {
	Server           string        `json:"server"`
	Token            string        `json:"token"`
	Executable       string        `json:"executable"`
	Arguments        []string      `json:"arguments,omitempty"`
	ElectronNodeMode bool          `json:"electronNodeMode,omitempty"`
	DataPath         string        `json:"dataPath"`
	Node             string        `json:"node"`
	InitScript       string        `json:"initScript"`
	Slots            []Credentials `json:"slots"`
}
type Credentials struct {
	MainUsername    string `json:"mainUsername"`
	MainPassword    string `json:"mainPassword"`
	MonitorUsername string `json:"monitorUsername"`
	MonitorPassword string `json:"monitorPassword"`
}

func LoadWorkerConfig(path string) (WorkerConfig, error) {
	var cfg WorkerConfig
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 256*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return cfg, errors.New("invalid observer configuration")
	}
	if d.Decode(new(any)) != io.EOF {
		return cfg, errors.New("invalid observer configuration suffix")
	}
	if cfg.Node == "" {
		cfg.Node = "node"
	}
	if len(cfg.Token) < 32 || len(cfg.Slots) < 1 || len(cfg.Slots) > 1000 || !filepath.IsAbs(cfg.Executable) || !filepath.IsAbs(cfg.DataPath) || !filepath.IsAbs(cfg.InitScript) {
		return cfg, errors.New("observer token, absolute paths and 1–1000 slots required")
	}
	seen := map[string]bool{}
	for _, s := range cfg.Slots {
		name := strings.ToLower(strings.TrimSpace(s.MonitorUsername))
		if name == "" || s.MonitorPassword == "" || s.MainUsername == "" || s.MainPassword == "" || strings.EqualFold(s.MainUsername, s.MonitorUsername) || seen[name] {
			return cfg, errors.New("each observer slot requires a distinct monitor login and a separate main login")
		}
		seen[name] = true
	}
	_, err = workerURL(cfg.Server)
	if err == nil {
		for _, path := range []string{cfg.Executable, cfg.InitScript} {
			if info, e := os.Stat(path); e != nil || info.IsDir() {
				return cfg, errors.New("observer runtime or initialization script is missing")
			}
		}
	}
	return cfg, err
}
func workerURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("invalid observer server URL")
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
		u.Scheme = "ws"
	} else {
		return "", errors.New("observer server requires HTTPS")
	}
	u.Path = "/internal/observer"
	return u.String(), nil
}

type slot struct {
	index    int
	base     string
	lease    Lease
	cancel   context.CancelFunc
	done     chan struct{}
	running  atomic.Bool
	restarts atomic.Uint64
}
type pool struct {
	cfg       WorkerConfig
	mu        sync.Mutex
	slots     []*slot
	positions chan Frame
	runner    func(context.Context, *slot) // optional deterministic process lifecycle test seam
}

func RunWorker(ctx context.Context, cfg WorkerConfig) error {
	endpoint, err := workerURL(cfg.Server)
	if err != nil {
		return err
	}
	p := &pool{cfg: cfg, positions: make(chan Frame, 128)}
	defer p.stop()
	delay := time.Second
	for ctx.Err() == nil {
		d, stop := context.WithTimeout(ctx, 8*time.Second)
		conn, _, err := websocket.Dial(d, endpoint, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + cfg.Token}}})
		stop()
		if err == nil {
			started := time.Now()
			err = p.session(ctx, conn)
			conn.CloseNow()
			p.stop() // never keep observing users after losing their authorization plan
			if time.Since(started) > 30*time.Second {
				delay = time.Second
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		log.Print("Observer connection interrupted; retrying") // no remote errors/credentials in logs
		timer := time.NewTimer(delay + time.Duration(rand.IntN(500))*time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
	return nil
}

func (p *pool) session(ctx context.Context, conn *websocket.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn.SetReadLimit(256 * 1024)
	write := func(f Frame) error {
		d, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		return wsjson.Write(d, conn, f)
	}
	if err := write(Frame{Type: "hello", Capacity: len(p.cfg.Slots)}); err != nil {
		return err
	}
	queries := make(chan Frame, 16)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	workers.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-p.positions:
				if write(f) != nil {
					cancel()
					return
				}
			}
		}
	})
	workers.Go(func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				health := p.health(ctx)
				if write(Frame{Type: "health", Health: &health}) != nil {
					cancel()
					return
				}
			}
		}
	})
	// Bound profile lookup concurrency independently of rider subscriptions.
	for range 2 {
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case f := <-queries:
					profiles, err := p.lookup(ctx, f)
					result := Frame{Type: "result", ID: f.ID, Profiles: profiles}
					if err != nil {
						result.Error = "profile unavailable"
					}
					if write(result) != nil {
						cancel()
						return
					}
				}
			}
		})
	}
	for {
		var f Frame
		if err := wsjson.Read(ctx, conn, &f); err != nil {
			return err
		}
		switch f.Type {
		case "plan":
			if err := p.reconcile(ctx, f.Leases); err != nil {
				return err
			}
		case "search", "profile":
			select {
			case queries <- f:
			default:
				if write(Frame{Type: "result", ID: f.ID, Error: "busy"}) != nil {
					return ErrUnavailable
				}
			}
		}
	}
}

func (p *pool) stop() {
	p.mu.Lock()
	slots := p.slots
	p.slots = nil
	p.mu.Unlock()
	for _, s := range slots {
		s.cancel()
	}
	for _, s := range slots {
		<-s.done
	}
	for {
		select {
		case <-p.positions:
		default:
			return
		}
	}
}
func (p *pool) reconcile(ctx context.Context, leases []Lease) error {
	if len(leases) > len(p.cfg.Slots) {
		return errors.New("observer plan exceeds capacity")
	}
	wanted := map[string]Lease{}
	athletes := map[int64]bool{}
	for _, l := range leases {
		if _, duplicate := wanted[l.ID]; duplicate || l.ID == "" || l.AthleteID <= 0 || athletes[l.AthleteID] {
			return errors.New("invalid observer plan")
		}
		wanted[l.ID] = l
		athletes[l.AthleteID] = true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	kept := make([]*slot, 0, len(p.cfg.Slots))
	used := map[int]bool{}
	for _, s := range p.slots {
		if len(leases) == 0 && s.lease.ID == "" {
			kept = append(kept, s)
			used[s.index] = true
			continue
		}
		if l, ok := wanted[s.lease.ID]; ok && l.AthleteID == s.lease.AthleteID {
			kept = append(kept, s)
			used[s.index] = true
			delete(wanted, s.lease.ID)
		} else {
			s.cancel()
			<-s.done
		}
	}
	p.slots = kept
	for _, l := range leases {
		if _, ok := wanted[l.ID]; !ok {
			continue
		}
		i := 0
		for used[i] {
			i++
		}
		used[i] = true
		p.launchLocked(ctx, i, l)
	}
	// One idle process is retained solely for name lookup and link verification.
	// It consumes a pool slot, but yields that slot as soon as a ride starts.
	if len(p.slots) == 0 {
		p.launchLocked(ctx, 0, Lease{})
	}
	return nil
}
func (p *pool) launchLocked(ctx context.Context, index int, lease Lease) {
	ctx, cancel := context.WithCancel(ctx)
	s := &slot{index: index, base: fmt.Sprintf("http://127.0.0.1:%d", 12080+index*2), lease: lease, cancel: cancel, done: make(chan struct{})}
	p.slots = append(p.slots, s)
	go func() {
		defer close(s.done)
		if p.runner != nil {
			p.runner(ctx, s)
		} else {
			p.runSlot(ctx, s)
		}
	}()
}

func (p *pool) runSlot(ctx context.Context, s *slot) {
	credentials := p.cfg.Slots[s.index]
	home := filepath.Join(p.cfg.DataPath, strconv.Itoa(s.index))
	delay := time.Second
	for ctx.Err() == nil {
		if err := p.initializeSlot(ctx, s.index, home); err != nil {
			log.Printf("Observer slot %d could not initialize; retrying", s.index)
			s.restarts.Add(1)
			if !waitRetry(ctx, delay) {
				return
			}
			delay = min(delay*2, 30*time.Second)
			continue
		}
		args := append(append([]string{}, p.cfg.Arguments...), "--headless", "--disable-game-connection")
		if s.lease.AthleteID > 0 {
			args = append(args, "--athlete-id", strconv.FormatInt(s.lease.AthleteID, 10))
		}
		cmd := exec.CommandContext(ctx, p.cfg.Executable, args...)
		// Do not pass broker tokens, hosting credentials or other slots' secrets
		// to Sauce. Stdout/stderr may contain remote profile data, so suppress it.
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "NODE_ENV=production", "MAIN_USERNAME=" + credentials.MainUsername, "MAIN_PASSWORD=" + credentials.MainPassword, "MON_USERNAME=" + credentials.MonitorUsername, "MON_PASSWORD=" + credentials.MonitorPassword}
		// This is the same Node mode used by Sauce's official headless launcher;
		// invoking that child directly avoids retaining an idle Electron parent.
		if p.cfg.ElectronNodeMode {
			cmd.Env = append(cmd.Env, "ELECTRON_RUN_AS_NODE=1")
		}
		cmd.Dir = filepath.Dir(p.cfg.Executable)
		cmd.WaitDelay = 3 * time.Second
		started := time.Now()
		if err := cmd.Start(); err != nil {
			log.Printf("Observer slot %d could not start", s.index)
		} else {
			s.running.Store(true)
			watchCtx, stop := context.WithCancel(ctx)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				if s.lease.AthleteID == 0 {
					<-watchCtx.Done()
					return
				}
				sauce.Watch(watchCtx, s.base, s.lease.AthleteID, func(a sauce.Athlete) {
					f := Frame{Type: "position", Position: &Position{Lease: s.lease.ID, Athlete: a, ObservedAt: time.Now().UnixMilli()}}
					select {
					case p.positions <- f:
					default:
					} // drop instead of accumulating stale audio positions
				}, func(string) {})
			}()
			_ = cmd.Wait()
			s.running.Store(false)
			stop()
			<-finished
		}
		if ctx.Err() != nil {
			return
		}
		log.Printf("Observer slot %d stopped; restarting", s.index)
		s.restarts.Add(1)
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		if !waitRetry(ctx, delay) {
			return
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func waitRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (p *pool) initializeSlot(ctx context.Context, index int, home string) error {
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	d, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(d, p.cfg.Node, p.cfg.InitScript, home, strconv.Itoa(12080+index*2))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}
	cmd.WaitDelay = time.Second
	return cmd.Run()
}

var sauceHTTP = &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (p *pool) lookup(ctx context.Context, f Frame) ([]Profile, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	p.mu.Lock()
	bases := make([]string, 0, len(p.slots))
	for _, s := range p.slots {
		bases = append(bases, s.base)
	}
	p.mu.Unlock()
	method := "getAthlete"
	var args any = []any{f.AthleteID, map[string]bool{"refresh": true, "allowFetch": true}}
	if f.Type == "search" {
		method = "searchAthletes"
		args = []any{f.Query, map[string]int{"limit": 25, "pageLimit": 1}}
	}
	body, _ := json.Marshal(args)
	for _, base := range bases {
		req, _ := http.NewRequestWithContext(ctx, "POST", base+"/api/rpc/v1/"+method, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := sauceHTTP.Do(req)
		if err != nil {
			continue
		}
		var envelope struct {
			Success bool            `json:"success"`
			Value   json.RawMessage `json:"value"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 512*1024)).Decode(&envelope)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 || !envelope.Success {
			continue
		}
		type profile struct {
			ID        int64  `json:"id"`
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
			Name      string `json:"name"`
		}
		format := func(v profile) Profile {
			n := strings.TrimSpace(v.FirstName + " " + v.LastName)
			if n == "" {
				n = v.Name
			}
			return Profile{ID: v.ID, Name: n}
		}
		if f.Type == "search" {
			var rows []struct {
				ID      int64   `json:"id"`
				Profile profile `json:"profile"`
			}
			if json.Unmarshal(envelope.Value, &rows) != nil {
				return nil, ErrUnavailable
			}
			result := make([]Profile, 0, min(len(rows), 25))
			for _, r := range rows {
				if r.ID > 0 && r.ID <= 9007199254740991 {
					r.Profile.ID = r.ID
					result = append(result, format(r.Profile))
					if len(result) == 25 {
						break
					}
				}
			}
			return result, nil
		}
		var v profile
		if json.Unmarshal(envelope.Value, &v) != nil || v.ID != f.AthleteID {
			return nil, ErrUnavailable
		}
		return []Profile{format(v)}, nil
	}
	return nil, ErrUnavailable
}
