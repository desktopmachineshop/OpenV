package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// setEarly sets the Content-Type once for every branch: a bare helper in
// a branch would misname the response.
func setEarly(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Query().Get("x") != "" {
		json.NewEncoder(w).Encode(map[string]string{"x": "set"})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"x": ""})
}

// withRetry writes another header between the Content-Type and the status.
func withRetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(3))
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]string{"error": "slow down"})
}

// withCharset sets another Content-Type.
func withCharset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode([]int{1, 2})
}

// lateType sets the Content-Type after the status, where it does nothing.
func lateType(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode([]int{3})
}

// commented has a comment inside the sequence, which a rewrite would drop.
func commented(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// The status is always 200 here.
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode("ok")
}

// readsWriter encodes a value that reads the writer: the helper would read
// it before the Content-Type is set.
func readsWriter(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"type": w.Header().Get("Content-Type")})
}

// callsClosure encodes the result of a function value, which could write
// the response.
func callsClosure(w http.ResponseWriter, r *http.Request) {
	body := func() string { w.Header().Set("X-Late", "1"); return "late" }
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(body())
}

// checked uses Encode's error.
func checked(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode([]string{"a"}); err != nil {
		fmt.Println(err)
	}
}

// indented keeps the encoder to configure it.
func indented(w http.ResponseWriter, r *http.Request) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]int{"n": 1})
}

// toBuffer encodes into a buffer, not the response.
func toBuffer(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode("buffered")
	w.Write(buf.Bytes())
}

// localWriter encodes to a writer that is not a parameter.
func localWriter(w http.ResponseWriter, r *http.Request) {
	out := w
	json.NewEncoder(out).Encode("local")
}

// stream is an SSE handler, which X1 does not touch.
func stream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode("event")
}

// loggedDecode answers through another writer: its literal becomes the
// constant, the call stays.
func loggedDecode(w http.ResponseWriter, r *http.Request) {
	var req item
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorCode(w, http.StatusBadRequest, "invalid request body", "bad_body")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// passThrough answers the decode error's own text (quirk Q19): untouched.
func passThrough(w http.ResponseWriter, r *http.Request) {
	var req item
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeJSONErrorCode is writeJSONError with a code.
func writeJSONErrorCode(w http.ResponseWriter, status int, message, code string) {
	writeJSONError(w, status, message+" ("+code+")")
}
