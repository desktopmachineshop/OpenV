package api

import (
	"encoding/json"
	"net/http"
)

// bareList answers with no Content-Type (quirk Q1). After the rewrite the
// file no longer names encoding/json, and drops its import.
func bareList(w http.ResponseWriter, r *http.Request) {
	items := []string{"<a>", "b&c"}
	if r.URL.Query().Get("empty") != "" {
		items = nil
	}
	json.NewEncoder(w).Encode(items)
}
