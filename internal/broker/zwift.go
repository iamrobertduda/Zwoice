package broker

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"github.com/iamrobertduda/Zwoice/internal/observer"
)

func (h *Hub) zwiftHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "use the Zwoice companion", 403)
		return
	}
	if h.cfg.Accounts == nil {
		accountError(w, accounts.ErrUnauthorized)
		return
	}
	if !h.allowAuth(r) {
		accountError(w, accounts.ErrRate)
		return
	}
	token := bearer(r)
	a, _, err := h.cfg.Accounts.Authenticate(token)
	if err != nil {
		accountError(w, err)
		return
	}
	if r.URL.Path == "/v1/zwift/unlink" {
		if err := h.cfg.Accounts.UnlinkZwift(token); err != nil {
			accountError(w, err)
			return
		}
		jsonResult(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.URL.Path == "/v1/zwift/status" {
		state := "unavailable"
		if a.Zwift == nil {
			state = "unlinked"
		} else if h.cfg.Observer != nil && h.cfg.Observer.Status().Connected {
			state = "waiting"
			if _, fresh := h.cfg.Observer.Latest(a.Zwift.AthleteID); fresh {
				state = "ready"
			}
		}
		jsonResult(w, 200, map[string]any{"state": state, "zwift": a.Zwift})
		return
	}
	if h.cfg.Observer == nil || !h.cfg.Observer.Status().Connected {
		accountError(w, observer.ErrUnavailable)
		return
	}
	switch r.URL.Path {
	case "/v1/zwift/search":
		var input struct {
			Query string `json:"query"`
		}
		if !decode(w, r, &input) {
			return
		}
		query := strings.TrimSpace(input.Query)
		if utf8.RuneCountInString(query) < 3 || len(query) > 120 {
			jsonResult(w, 400, map[string]string{"message": "Gib mindestens drei Buchstaben deines Zwift-Namens ein."})
			return
		}
		var profiles []observer.Profile
		// Pasted numeric rider IDs are accepted as an optional shortcut; normal
		// users search by the name shown in Zwift.
		if id, e := strconv.ParseInt(query, 10, 64); e == nil && id > 0 && id <= 9007199254740991 {
			p, e := h.cfg.Observer.Profile(r.Context(), id)
			err = e
			if e == nil {
				profiles = []observer.Profile{p}
			}
		} else {
			profiles, err = h.cfg.Observer.Search(r.Context(), query)
		}
		if err != nil {
			accountError(w, err)
			return
		}
		jsonResult(w, 200, map[string]any{"profiles": profiles})
	case "/v1/zwift/begin":
		var input struct {
			AthleteID int64 `json:"athleteId"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.AthleteID <= 0 || input.AthleteID > 9007199254740991 {
			accountError(w, accounts.ErrZwiftLink)
			return
		}
		profile, err := h.cfg.Observer.Profile(r.Context(), input.AthleteID)
		if err != nil {
			accountError(w, err)
			return
		}
		challenge, err := h.cfg.Accounts.BeginZwiftLink(token, profile.ID)
		if err != nil {
			accountError(w, err)
			return
		}
		jsonResult(w, 200, map[string]any{"challenge": challenge, "profile": profile})
	case "/v1/zwift/confirm":
		challenge, err := h.cfg.Accounts.ZwiftChallenge(token)
		if err != nil {
			accountError(w, err)
			return
		}
		profile, err := h.cfg.Observer.Profile(r.Context(), challenge.AthleteID)
		if err != nil {
			accountError(w, err)
			return
		}
		if err := h.cfg.Accounts.ConfirmZwiftLink(token, challenge, profile.ID, profile.Name); err != nil {
			accountError(w, err)
			return
		}
		a, _, err := h.cfg.Accounts.Authenticate(token)
		if err != nil {
			accountError(w, err)
			return
		}
		jsonResult(w, 200, map[string]any{"account": a})
	default:
		http.NotFound(w, r)
	}
}
