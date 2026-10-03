package api

import (
	"encoding/json"
	"net/http"
)

// respondJSON writes a status and a JSON body, which every handler here does.
func respondJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
