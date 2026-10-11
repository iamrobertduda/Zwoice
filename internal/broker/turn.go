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
)

// Permanent Cloudflare credentials stay in the signaling service. Each session
// receives only ephemeral credentials, renewed after six of their twelve hours.
func (h *Hub) fetchICE(ctx context.Context, id string) ([]map[string]any, error) {
	if h.cfg.CloudflareKeyID == "" {
		return h.iceServers(id, time.Now()), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	endpoint := "https://rtc.live.cloudflare.com/v1/turn/keys/" + url.PathEscape(h.cfg.CloudflareKeyID) + "/credentials/generate-ice-servers"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(`{"ttl":43200}`))
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
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		return nil, errors.New("TURN credentials unavailable")
	}
	var data struct {
		ICEServers []map[string]any `json:"iceServers"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 32*1024)).Decode(&data) != nil || len(data.ICEServers) == 0 {
		return nil, errors.New("invalid TURN response")
	}
	return data.ICEServers, nil
}
