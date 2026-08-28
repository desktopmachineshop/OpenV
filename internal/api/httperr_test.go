package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRespondInternalSanitizesBodyAndLogsRealError is the representative test
// for the internal-error sweep: the client sees only the stable public
// message in the JSON envelope, while the real error (which may carry SQL
// text, file paths, or upstream details) reaches the server log at ERROR.
func TestRespondInternalSanitizesBodyAndLogsRealError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/things", nil)
	dbErr := errors.New(`pq: syntax error near "SELECT token_hash FROM secrets"`)

	respondInternal(w, r, "failed to list things", dbErr)

	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the JSON error envelope: %v (body %q)", err, w.Body.String())
	}
	if body.Error != "failed to list things" {
		t.Errorf("body error = %q, want the public message only", body.Error)
	}
	if strings.Contains(w.Body.String(), "token_hash") {
		t.Error("response body leaked internal error detail")
	}

	logged := buf.String()
	if !strings.Contains(logged, "token_hash") {
		t.Errorf("server log should carry the real error, got %q", logged)
	}
	if !strings.Contains(logged, "failed to list things") {
		t.Errorf("server log should carry the public message, got %q", logged)
	}
	if !strings.Contains(logged, "level=ERROR") {
		t.Errorf("5xx should log at ERROR, got %q", logged)
	}
}

// TestRespondErrorWarnsOnClientErrors pins the 4xx logging level and the
// pass-through of user-facing messages.
func TestRespondErrorWarnsOnClientErrors(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/things/nope", nil)

	respondError(w, r, 404, "thing not found", errors.New("sql: no rows in result set"))

	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the JSON error envelope: %v (body %q)", err, w.Body.String())
	}
	if body.Error != "thing not found" {
		t.Errorf("body error = %q, want the public message only", body.Error)
	}
	if strings.Contains(w.Body.String(), "sql:") {
		t.Error("response body leaked the underlying error")
	}
	if logged := buf.String(); !strings.Contains(logged, "level=WARN") {
		t.Errorf("4xx should log at WARN, got %q", logged)
	}
}
