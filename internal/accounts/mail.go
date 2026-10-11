package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Resend sends a transactional code only after the user requests a login.
// The interface is replaced by an in-memory mailbox in tests.
type Resend struct {
	APIKey, From string
	Client       *http.Client
	Endpoint     string
}

func (m Resend) SendLogin(ctx context.Context, email, code, id string) error {
	endpoint := m.Endpoint
	if endpoint == "" {
		endpoint = "https://api.resend.com/emails"
	}
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	payload, _ := json.Marshal(map[string]any{"from": m.From, "to": []string{email}, "subject": "Dein Zwoice-Anmeldecode", "text": fmt.Sprintf("Dein Code für Zwoice: %s\n\nGib diesen Code in Zwoice ein. Er gilt 10 Minuten und funktioniert nur einmal. Beim ersten Anmelden wird dein Konto automatisch erstellt.\n\nWenn du diese Anmeldung nicht angefordert hast, ignoriere diese E-Mail. Teile den Code nicht mit anderen.", code)})
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "zwoice-login-"+id)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ErrMail
	}
	return nil
}
