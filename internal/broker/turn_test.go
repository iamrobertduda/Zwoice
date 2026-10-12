package broker

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"github.com/iamrobertduda/Zwoice/internal/auth"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTURNRequiresLiveAccountAndRevokes(t *testing.T) {
	store, token, a := accountFixture(t)
	var generated, revoked atomic.Int64
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cf-secret" {
			t.Error("missing CF authorization")
		}
		if strings.HasSuffix(r.URL.Path, "/revoke") {
			revoked.Add(1)
			w.WriteHeader(204)
			return
		}
		generated.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"iceServers": []map[string]any{{"urls": []string{"turn:example.test:3478"}, "username": "scoped-user", "credential": "ephemeral-secret"}}})
	}))
	defer cloud.Close()
	hub := New(Config{Accounts: store, Observer: connectedObserver(t), CloudflareKeyID: "key", CloudflareAPIToken: "cf-secret", TURNEndpoint: cloud.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	server := httptest.NewServer(hub.Handler())
	defer server.Close()
	address := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/session"
	legacy, _ := auth.Sign([]byte(strings.Repeat("s", 32)), auth.Claims{AthleteID: 100, Cohort: "public", Expires: time.Now().Add(time.Hour).Unix()})
	for _, bad := range []string{"", strings.Repeat("a", 43), legacy} {
		headers := http.Header{"Zwoice-Athlete-ID": []string{"100"}}
		if bad != "" {
			headers.Set("Authorization", "Bearer "+bad)
		}
		c, res, e := websocket.Dial(ctx, address, &websocket.DialOptions{HTTPHeader: headers})
		if c != nil {
			c.CloseNow()
		}
		if e == nil || res.StatusCode != 401 {
			t.Fatal("invalid account accepted", res.StatusCode, e)
		}
	}
	if generated.Load() != 0 {
		t.Fatal("unauthenticated user consumed TURN")
	}
	c, _, e := websocket.Dial(ctx, address, &websocket.DialOptions{HTTPHeader: http.Header{"Zwoice-Athlete-ID": []string{"100"}, "Authorization": []string{"Bearer " + token}}})
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	var welcome map[string]any
	if e = wsjson.Read(ctx, c, &welcome); e != nil || welcome["type"] != "welcome" || generated.Load() != 1 {
		t.Fatal("valid login did not receive TURN", e)
	}
	if e = store.SetAccess(token, a.ID, false, false, 144); e != nil {
		t.Fatal(e)
	}
	deadline, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	for {
		var message any
		if wsjson.Read(deadline, c, &message) != nil {
			break
		}
	}
	if deadline.Err() != nil {
		t.Fatal("TURN ban did not terminate active session")
	}
	if _, e = hub.fetchICE(ctx, "100", token, "renew"); e != accounts.ErrTURN {
		t.Fatal("banned account renewed TURN", e)
	}
	until := time.Now().Add(5 * time.Second)
	for revoked.Load() == 0 && time.Now().Before(until) {
		time.Sleep(20 * time.Millisecond)
	}
	if revoked.Load() == 0 || generated.Load() != 1 {
		t.Fatal("missing cloud revocation or unauthorized generation", revoked.Load(), generated.Load())
	}
}
func TestLogoutDuringTURNGeneration(t *testing.T) {
	store, token, _ := accountFixture(t)
	var revoked atomic.Int64
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/revoke") {
			revoked.Add(1)
			w.WriteHeader(503)
			return
		}
		store.Logout(token)
		w.Write([]byte(`{"iceServers":[{"username":"orphan","credential":"secret","urls":["turn:example.test"]}]}`))
	}))
	defer cloud.Close()
	hub := New(Config{Accounts: store, CloudflareKeyID: "key", TURNEndpoint: cloud.URL})
	ice, e := hub.fetchICE(context.Background(), "100", token, "connection")
	if e == nil || len(ice) != 0 || revoked.Load() != 1 {
		t.Fatal("logout raced past TURN gate", ice, e)
	}
	// A failed remote revocation is persisted, including after a restart.
	if e = store.QueueRevocation("orphan", time.Now().Add(time.Hour).Unix()); e != nil {
		t.Fatal(e)
	}
	pending, e := store.PendingRevocations(10)
	if e != nil || len(pending) != 1 {
		t.Fatal("lost orphan credential", pending, e)
	}
}

// Opt-in contract test against the configured real provider; secrets never enter output.
func TestCloudflareCredentialLifecycle(t *testing.T) {
	key, secret := os.Getenv("ZWOICE_TEST_CF_KEY"), os.Getenv("ZWOICE_TEST_CF_TOKEN")
	if key == "" || secret == "" {
		t.Skip("real provider credentials not configured")
	}
	store, token, _ := accountFixture(t)
	hub := New(Config{Accounts: store, CloudflareKeyID: key, CloudflareAPIToken: secret})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ice, err := hub.fetchICE(ctx, "1001", token, "contract-test")
	if err != nil {
		t.Fatal("provider credential generation failed")
	}
	seen := map[string]bool{}
	for _, server := range ice {
		username, _ := server["username"].(string)
		if username == "" || seen[username] {
			continue
		}
		seen[username] = true
		if err := hub.revokeCredential(ctx, username); err != nil {
			t.Fatal("provider rejected credential revocation")
		}
		store.RevocationResult(username, true)
	}
	if len(seen) == 0 {
		t.Fatal("provider returned no scoped credential")
	}
}
