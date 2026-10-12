package broker

import (
	"context"
	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"path/filepath"
	"testing"
)

type testMail struct{ code string }

func (m *testMail) SendLogin(_ context.Context, _ string, code, _ string) error {
	m.code = code
	return nil
}
func accountFixture(t *testing.T) (*accounts.Store, string, accounts.Account) {
	t.Helper()
	mail := &testMail{}
	store, err := accounts.Open(accounts.Config{Path: filepath.Join(t.TempDir(), "accounts.db"), Mailer: mail, AdminEmail: "test@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	id, err := store.RequestCode(context.Background(), "test@example.test")
	if err != nil {
		t.Fatal(err)
	}
	token, a, _, err := store.VerifyCode(id, mail.code, true)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.BeginZwiftLink(token, 123)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmZwiftLink(token, ch, 123, "Test Rider "+ch.Code); err != nil {
		t.Fatal(err)
	}
	a, _, _ = store.Authenticate(token)
	return store, token, a
}
