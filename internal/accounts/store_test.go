package accounts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type mailbox struct {
	mu    sync.Mutex
	codes map[string]string
}

func (m *mailbox) SendLogin(_ context.Context, e, c, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codes == nil {
		m.codes = map[string]string{}
	}
	m.codes[id] = c
	return nil
}
func (m *mailbox) code(id string) string { m.mu.Lock(); defer m.mu.Unlock(); return m.codes[id] }
func fixture(t *testing.T) (*Store, *mailbox, *time.Time) {
	t.Helper()
	now := time.Now()
	m := &mailbox{}
	s, e := Open(Config{Path: filepath.Join(t.TempDir(), "accounts.db"), AdminEmail: "owner@example.test", Mailer: m, Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, m, &now
}
func login(t *testing.T, s *Store, m *mailbox, email string, remember bool) (string, Account) {
	t.Helper()
	id, e := s.RequestCode(context.Background(), email)
	if e != nil {
		t.Fatal(e)
	}
	token, a, _, e := s.VerifyCode(id, m.code(id), remember)
	if e != nil {
		t.Fatal(e)
	}
	return token, a
}
func TestVerifiedAccountPersistenceAndReplay(t *testing.T) {
	dir := t.TempDir()
	m := &mailbox{}
	cfg := Config{Path: filepath.Join(dir, "accounts.db"), Mailer: m, AdminEmail: "owner@example.test"}
	s, e := Open(cfg)
	if e != nil {
		t.Fatal(e)
	}
	id, e := s.RequestCode(context.Background(), " Rider@Example.test ")
	if e != nil {
		t.Fatal(e)
	}
	code := m.code(id)
	token, a, expiry, e := s.VerifyCode(id, code, true)
	if e != nil || a.Email != "rider@example.test" || a.Admin || expiry < time.Now().Add(29*24*time.Hour).Unix() {
		t.Fatal(a, expiry, e)
	}
	if _, _, _, e = s.VerifyCode(id, code, true); !errors.Is(e, ErrCode) {
		t.Fatal("code replay", e)
	}
	if e = s.RecordGrant(token, "connection", "turn-user", time.Now().Add(time.Hour).Unix()); e != nil {
		t.Fatal(e)
	}
	s.Close()
	data, e := os.ReadFile(cfg.Path)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), token) || strings.Contains(string(data), `"Digest":"`+code+`"`) {
		t.Fatal("plaintext secret persisted")
	}
	s, e = Open(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if got, _, e := s.Authenticate(token); e != nil || got.ID != a.ID {
		t.Fatal("lost login on restart", e)
	}
	pending, e := s.PendingRevocations(10)
	if e != nil || len(pending) != 1 {
		t.Fatal("restart did not retire dead connection", pending, e)
	}
	if e = s.Logout(token); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Authenticate(token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("logout did not invalidate token", e)
	}
}
func TestCodesExpireAndAttemptsCommit(t *testing.T) {
	s, m, now := fixture(t)
	id, e := s.RequestCode(context.Background(), "rider@example.test")
	if e != nil {
		t.Fatal(e)
	}
	wrong := "00000000"
	if wrong == m.code(id) {
		wrong = "11111111"
	}
	for i := 0; i < 5; i++ {
		if _, _, _, e = s.VerifyCode(id, wrong, false); !errors.Is(e, ErrCode) {
			t.Fatal(e)
		}
	}
	if _, _, _, e = s.VerifyCode(id, m.code(id), false); !errors.Is(e, ErrCode) {
		t.Fatal("attempt limit bypass", e)
	}
	*now = now.Add(time.Hour)
	id, e = s.RequestCode(context.Background(), "rider@example.test")
	if e != nil {
		t.Fatal(e)
	}
	*now = now.Add(11 * time.Minute)
	if _, _, _, e = s.VerifyCode(id, m.code(id), false); !errors.Is(e, ErrCode) {
		t.Fatal("expired code", e)
	}
	token, _ := login(t, s, m, "valid@example.test", false)
	*now = now.Add(13 * time.Hour)
	if _, _, e = s.Authenticate(token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("expired session", e)
	}
}
func TestOperatorControlsAndRevocation(t *testing.T) {
	s, m, now := fixture(t)
	owner, _ := login(t, s, m, "owner@example.test", true)
	token, a := login(t, s, m, "rider@example.test", true)
	if e := s.SetAccess(token, a.ID, false, true, 2); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("non-admin changed access", e)
	}
	if e := s.SetPolicy(token, Policy{false, false}); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("non-admin policy", e)
	}
	if e := s.SetAccess(owner, a.ID, false, true, 2); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e := s.ReserveTURN(token); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.ReserveTURN(token); !errors.Is(e, ErrTURN) {
		t.Fatal("quota bypass", e)
	}
	*now = now.Add(24 * time.Hour)
	if e := s.ReserveTURN(token); e != nil {
		t.Fatal("quota did not reset", e)
	}
	if e := s.RecordGrant(token, "ride", "grant", now.Add(time.Hour).Unix()); e != nil {
		t.Fatal(e)
	}
	if e := s.SetAccess(owner, a.ID, false, false, 2); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Authenticate(token); e != nil {
		t.Fatal("TURN ban prevented account access", e)
	}
	if e := s.ReserveTURN(token); !errors.Is(e, ErrTURN) {
		t.Fatal("TURN ban bypass", e)
	}
	pending, _ := s.PendingRevocations(10)
	if len(pending) != 1 {
		t.Fatal("grant not revoked", pending)
	}
	if e := s.SetAccess(owner, a.ID, true, false, 2); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Authenticate(token); e == nil {
		t.Fatal("ban left session live")
	}
	if e := s.SetAccess(owner, a.ID, false, true, 2); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Authenticate(token); e == nil {
		t.Fatal("unban revived old session")
	}
	if e := s.SetPolicy(owner, Policy{false, false}); e != nil {
		t.Fatal(e)
	}
	id, e := s.RequestCode(context.Background(), "new@example.test")
	if e != nil || m.code(id) != "" {
		t.Fatal("closed signup sent mail", e)
	}
	if e := s.ReserveTURN(owner); !errors.Is(e, ErrTURN) {
		t.Fatal("global TURN gate bypass", e)
	}
	if list, _, e := s.List(owner, ""); e != nil || len(list) != 2 {
		t.Fatal("phantom unverified account", list, e)
	}
}
func TestSessionLimitRatesAndRevocationRetry(t *testing.T) {
	s, m, now := fixture(t)
	first, _ := login(t, s, m, "rider@example.test", true)
	if _, e := s.RequestCode(context.Background(), "rider@example.test"); !errors.Is(e, ErrRate) {
		t.Fatal("email cooldown bypass", e)
	}
	for i := 0; i < 5; i++ {
		*now = now.Add(time.Hour)
		login(t, s, m, "rider@example.test", true)
	}
	if _, _, e := s.Authenticate(first); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("oldest sixth device not retired", e)
	}
	if e := s.QueueRevocation("orphan", now.Add(time.Hour).Unix()); e != nil {
		t.Fatal(e)
	}
	s.RevocationResult("orphan", false)
	p, _ := s.PendingRevocations(10)
	if len(p) != 0 {
		t.Fatal("retry backoff bypass")
	}
	*now = now.Add(time.Minute)
	p, _ = s.PendingRevocations(10)
	if len(p) != 1 {
		t.Fatal("retry lost")
	}
	s.RevocationResult("orphan", true)
	p, _ = s.PendingRevocations(10)
	if len(p) != 0 {
		t.Fatal("completed revoke retained")
	}
}
