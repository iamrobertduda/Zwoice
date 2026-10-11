package accounts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransactionalMail(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("Idempotency-Key") != "zwoice-login-challenge" {
			t.Error("missing mail authorization/idempotency")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body["text"].(string), "12345678") {
			t.Error("missing code")
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"id":"mail"}`))
	}))
	defer server.Close()
	mail := &Resend{APIKey: "test", From: "Zwoice <login@example.test>", Endpoint: server.URL}
	if e := mail.SendLogin(context.Background(), "rider@example.test", "12345678", "challenge"); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
}
