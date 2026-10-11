package broker

import (
	"net"
	"net/http"
	"net/netip"
	"time"
)

// Network admission limits complement account verification. Sauce itself does
// not attest Zwift account ownership.
type admission struct {
	tokens  float64
	updated time.Time
	active  int
}

func (h *Hub) admissionIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	// Enable only behind Railway's HTTP ingress, which overwrites X-Real-IP.
	// Direct/self-hosted servers ignore caller-supplied forwarding headers.
	if h.cfg.TrustRailwayProxy {
		if ip, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil {
			host = ip.String()
		}
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	ip = ip.Unmap()
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String()
	}
	return ip.String()
}

// reservePublic includes pending TURN requests in both capacity and per-IP
// limits. It is called with h.mu held, and returns an HTTP status on rejection.
func (h *Hub) reservePublic(ip, id string, now time.Time) int {
	if _, ok := h.clients[id]; ok || h.pending[id] {
		return http.StatusConflict
	}
	if len(h.clients)+len(h.pending) >= h.cfg.MaxClients {
		return http.StatusServiceUnavailable
	}
	for key, entry := range h.admissions {
		if entry.active == 0 && now.Sub(entry.updated) > 10*time.Minute {
			delete(h.admissions, key)
		}
	}
	entry := h.admissions[ip]
	if entry == nil {
		if len(h.admissions) >= 10000 {
			return http.StatusTooManyRequests
		}
		entry = &admission{tokens: 20, updated: now}
		h.admissions[ip] = entry
	}
	entry.tokens = min(20, entry.tokens+now.Sub(entry.updated).Seconds()/10)
	entry.updated = now
	h.joinTokens = min(60, h.joinTokens+now.Sub(h.joinUpdated).Seconds()*2)
	h.joinUpdated = now
	if entry.active >= 8 || entry.tokens < 1 || h.joinTokens < 1 {
		return http.StatusTooManyRequests
	}
	entry.tokens--
	h.joinTokens--
	entry.active++
	h.pending[id] = true
	return 0
}

func (h *Hub) releasePublic(ip, id string, pending bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if pending {
		delete(h.pending, id)
	}
	if entry := h.admissions[ip]; entry != nil {
		entry.active--
	}
}
