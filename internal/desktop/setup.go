package desktop

import (
	"encoding/json"
	"net/http"

	"github.com/iamrobertduda/Zwoice/internal/sauce"
)

// Setup checks only the explicitly configured Sauce endpoint. No LAN scanning,
// microphone access, account creation or relay credentials are needed here.
func setup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Sauce string `json:"sauce"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&request) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if request.Sauce == "" {
		request.Sauce = "http://127.0.0.1:1080"
	}
	if _, err := sauce.ValidateURL(request.Sauce); err != nil {
		http.Error(w, "invalid Sauce address", 400)
		return
	}
	state, name := "missing", ""
	if athlete, err := sauce.ReadSelf(r.Context(), request.Sauce); err == nil {
		state = "waiting"
		if sample, err := sauce.Normalize(athlete); err == nil {
			state, name = "ready", sample.Name
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"state": state, "name": name})
}
