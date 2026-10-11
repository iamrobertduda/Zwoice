// Package sauce consumes the external Sauce API. No Sauce code is embedded.
package sauce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/proximity"
)

type Athlete struct {
	Version int      `json:"version"`
	ID      int64    `json:"athleteId"`
	Self    bool     `json:"self"`
	Age     *float64 `json:"age"`
	Profile *struct {
		Name      string `json:"name"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"athlete"`
	State *struct {
		X         *float64 `json:"x"`
		Y         *float64 `json:"y"`
		Z         *float64 `json:"z"`
		Course    int      `json:"courseId"`
		Event     int64    `json:"eventSubgroupId"`
		Sport     string   `json:"sport"`
		WorldTime int64    `json:"worldTime"`
	} `json:"state"`
}

type Sample struct {
	AthleteID int64              `json:"athleteId"`
	Position  proximity.Position `json:"position"`
	WorldTime int64              `json:"-"`
	Name      string             `json:"name"`
}

func Normalize(a Athlete) (Sample, error) {
	s := a.State
	if a.Version != 2 || !a.Self || a.ID <= 0 || a.Age == nil || *a.Age < 0 || *a.Age > 3000 || s == nil || s.X == nil || s.Y == nil || s.Z == nil || s.WorldTime <= 0 {
		return Sample{}, errors.New("Sauce liefert noch keine aktuelle eigene Fahrerposition")
	}
	// Sauce's PlayerState has no realm field; its live Zwift client uses realm 1.
	p := proximity.Position{X: *s.X / 100, Y: *s.Y / 100, Z: *s.Z / 100, Course: s.Course, Realm: 1, Event: s.Event, Sport: strings.ToUpper(s.Sport)}
	if !p.Valid() {
		return Sample{}, errors.New("ungültige Sauce-Position")
	}
	name := ""
	if a.Profile != nil {
		name = strings.TrimSpace(a.Profile.Name)
		if name == "" {
			name = strings.TrimSpace(a.Profile.FirstName + " " + a.Profile.LastName)
		}
	}
	return Sample{AthleteID: a.ID, Position: p, WorldTime: s.WorldTime, Name: name}, nil
}

func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("Sauce-Adresse muss eine HTTP(S)-Basisadresse sein, z. B. http://127.0.0.1:1080")
	}
	return u, nil
}

// Run reconnects until cancelled. Only a new worldTime refreshes a sample, so
// replayed/cached Sauce data cannot keep a stopped rider audible indefinitely.
func Run(ctx context.Context, base string, sample func(Sample), status func(string)) {
	delay := 500 * time.Millisecond
	for ctx.Err() == nil {
		started := time.Now()
		err := stream(ctx, base, sample)
		if ctx.Err() != nil {
			return
		}
		status(fmt.Sprintf("Sauce nicht verbunden: %v", err))
		if time.Since(started) > 10*time.Second {
			delay = 500 * time.Millisecond
		}
		timer := time.NewTimer(delay + time.Duration(rand.IntN(250))*time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 8*time.Second {
			delay *= 2
		}
	}
}

func stream(ctx context.Context, base string, sample func(Sample)) error {
	u, err := ValidateURL(base)
	if err != nil {
		return err
	}
	u.Path = "/api/ws/events"
	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else {
		u.Scheme = "wss"
	}
	dialCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	conn, _, err := websocket.Dial(dialCtx, u.String(), nil)
	cancel()
	if err != nil {
		return errors.New("API nicht erreichbar; Sauce starten und Webserver aktivieren")
	}
	defer conn.CloseNow()
	conn.SetReadLimit(256 * 1024)
	request := map[string]any{"type": "request", "uid": 1, "data": map[string]any{"method": "subscribe", "arg": map[string]any{"event": "athlete/self/v2", "subId": 2, "source": "stats", "options": map[string]any{"resources": []string{"state", "athlete"}, "stats": false}}}}
	writeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	err = wsjson.Write(writeCtx, conn, request)
	stop()
	if err != nil {
		return err
	}
	last := int64(0)
	accept := func(a Athlete) {
		if s, e := Normalize(a); e == nil && s.WorldTime > last {
			last = s.WorldTime
			sample(s)
		}
	}
	// Seed a newly opened session without waiting for the next game packet.
	if a, e := ReadSelf(ctx, base); e == nil {
		accept(a)
	}
	for {
		var env struct {
			Type    string          `json:"type"`
			UID     int             `json:"uid"`
			Success bool            `json:"success"`
			Data    json.RawMessage `json:"data"`
		}
		readCtx, stop := context.WithTimeout(ctx, 12*time.Second)
		err = wsjson.Read(readCtx, conn, &env)
		stop()
		if err != nil {
			return errors.New("Datenstrom unterbrochen")
		}
		if !env.Success {
			return errors.New("Sauce unterstützt das angeforderte self/v2-Abonnement nicht")
		}
		if env.Type == "event" && env.UID == 2 {
			var a Athlete
			if json.Unmarshal(env.Data, &a) == nil {
				accept(a)
			}
		}
	}
}

// ReadSelf is a bounded readiness check; it does not open a voice session.
func ReadSelf(ctx context.Context, base string) (Athlete, error) {
	var a Athlete
	u, err := ValidateURL(base)
	if err != nil {
		return a, err
	}
	u.Path = "/api/athlete/v2/self"
	u.RawQuery = "resource=state&resource=athlete"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return a, err
	}
	defer res.Body.Close()
	// An empty /self (404) means Sauce is reachable but the ride has not begun.
	if res.StatusCode == http.StatusNotFound {
		return a, nil
	}
	if res.StatusCode != http.StatusOK {
		return a, errors.New("Sauce-Webserver nicht verfügbar")
	}
	err = json.NewDecoder(http.MaxBytesReader(nil, res.Body, 256*1024)).Decode(&a)
	return a, err
}
