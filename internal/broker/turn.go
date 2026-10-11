package broker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iamrobertduda/Zwoice/internal/accounts"
)

func (h *Hub) turnBase() string {
	if h.cfg.TURNEndpoint != "" {
		return h.cfg.TURNEndpoint
	}
	return "https://rtc.live.cloudflare.com/v1/turn/keys/" + url.PathEscape(h.cfg.CloudflareKeyID)
}
func (h *Hub) fetchICE(ctx context.Context, id, token, connectionID string) ([]map[string]any, error) {
	if h.cfg.Accounts == nil {
		return nil, accounts.ErrUnauthorized
	}
	if err := h.cfg.Accounts.ReserveTURN(token); err != nil {
		return nil, err
	}
	if h.cfg.CloudflareKeyID == "" {
		return h.iceServers(id, time.Now()), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", h.turnBase()+"/credentials/generate-ice-servers", strings.NewReader(`{"ttl":3600}`))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+h.cfg.CloudflareAPIToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("TURN credentials unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 201 {
		return nil, errors.New("TURN credentials unavailable")
	}
	var data struct {
		ICEServers []map[string]any `json:"iceServers"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 32*1024)).Decode(&data) != nil || len(data.ICEServers) == 0 {
		return nil, errors.New("invalid TURN response")
	}
	// Persist the account/session association before exposing any relay secret.
	// If access changed during generation, revoke the new credential immediately.
	seen := map[string]bool{}
	for _, server := range data.ICEServers {
		username, ok := server["username"].(string)
		if !ok || username == "" || seen[username] {
			continue
		}
		seen[username] = true
		if err := h.cfg.Accounts.RecordGrant(token, connectionID, username, time.Now().Add(time.Hour).Unix()); err != nil {
			_ = h.cfg.Accounts.QueueRevocation(username, time.Now().Add(time.Hour).Unix())
			revokeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			revoked := h.revokeCredential(revokeCtx, username) == nil
			_ = h.cfg.Accounts.RevocationResult(username, revoked)
			stop()
			return nil, err
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("TURN response contains no credential")
	}
	return data.ICEServers, nil
}
func (h *Hub) revokeCredential(ctx context.Context, username string) error {
	if h.cfg.CloudflareKeyID == "" {
		return nil
	} // coturn expiry is its fallback boundary.
	req, err := http.NewRequestWithContext(ctx, "POST", h.turnBase()+"/credentials/"+url.PathEscape(username)+"/revoke", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.cfg.CloudflareAPIToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 204 && res.StatusCode != 404 {
		return errors.New("TURN revocation pending")
	}
	return nil
}
func (h *Hub) revokeLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastSweep := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(lastSweep) > time.Minute {
				_ = h.cfg.Accounts.Sweep()
				lastSweep = time.Now()
			}
			pending, err := h.cfg.Accounts.PendingRevocations(8)
			if err != nil {
				continue
			}
			for _, grant := range pending {
				if ctx.Err() != nil {
					return
				}
				c, stop := context.WithTimeout(ctx, 3*time.Second)
				err := h.revokeCredential(c, grant.Username)
				stop()
				_ = h.cfg.Accounts.RevocationResult(grant.Username, err == nil)
			}
		}
	}
}
