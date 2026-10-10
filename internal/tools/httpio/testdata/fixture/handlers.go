package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type item struct {
	ID    string            `json:"id"`
	Title string            `json:"title"`
	Tags  []string          `json:"tags"`
	Meta  map[string]string `json:"meta,omitempty"`
}

type store struct{ items []item }

func (s *store) find(id string) *item {
	for i := range s.items {
		if s.items[i].ID == id {
			return &s.items[i]
		}
	}
	return nil
}

var shop = &store{items: []item{{ID: "1", Title: "<b>one</b> & more", Tags: nil}}}

// createItem: Content-Type, status, encode (writeJSON), and a decode
// answered "invalid request body" (decodeJSON).
func createItem(w http.ResponseWriter, r *http.Request) {
	var req item
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ID = "2"
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}

// getItem: Content-Type then encode (writeJSONOK), the encode's error
// dropped with `_ =`, and a value built from a call.
func getItem(w http.ResponseWriter, r *http.Request) {
	it := shop.find(r.URL.Query().Get("id"))
	if it == nil {
		writeJSONError(w, http.StatusNotFound, "item not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"item":  it,
		"upper": strings.ToUpper(it.Title),
	})
}

// acceptItem: status then encode with no Content-Type (writeJSONBareStatus),
// in a switch case, the status a variable.
func acceptItem(w http.ResponseWriter, r *http.Request) {
	code := http.StatusAccepted
	switch r.URL.Query().Get("mode") {
	case "later":
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
	default:
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(nil)
	}
}

// renameItem: decodeJSONMsg for a message of its own (quirk Q19), with a
// 400 written as a number, inside a function literal that captures w.
func renameItem(w http.ResponseWriter, r *http.Request) {
	do := func() {
		var req struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, 400, "Invalid request body")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(req)
	}
	do()
}

// decodeFilter returns what a helper returns when the decode fails.
func decodeFilter(w http.ResponseWriter, r *http.Request) (item, bool) {
	var f item
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return item{}, false
	}
	return f, true
}

// filterItems uses decodeFilter, then answers the filter back.
func filterItems(w http.ResponseWriter, r *http.Request) {
	f, ok := decodeFilter(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(f)
}

// optionalBody allows an empty body: not decodeJSON's shape, so its
// literal becomes the constant.
func optionalBody(w http.ResponseWriter, r *http.Request) {
	var req item
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(req)
}

// rawBody reads the body itself: its literal becomes the constant too.
func rawBody(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) == 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	json.NewEncoder(w).Encode(map[string]int{"bytes": len(data)})
}
