package desktop

import (
	"encoding/json"
	"net/http"
)

// Setup reads central readiness after login. No local Sauce connection exists.
func setup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Server string `json:"server"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&request) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	base, err := accountBase(request.Server)
	if err != nil {
		http.Error(w, "invalid server", 400)
		return
	}
	token := sessionToken(r, base)
	w.Header().Set("Content-Type", "application/json")
	if token == "" {
		json.NewEncoder(w).Encode(map[string]string{"state": "login"})
		return
	}
	result, status, err := accountCall(r.Context(), base, "/v1/zwift/status", token, []byte(`{}`))
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"state": "unavailable"})
		return
	}
	if status == 401 || status == 403 {
		json.NewEncoder(w).Encode(map[string]string{"state": "login"})
		return
	}
	if status != 200 {
		json.NewEncoder(w).Encode(map[string]string{"state": "unavailable"})
		return
	}
	json.NewEncoder(w).Encode(result)
}
