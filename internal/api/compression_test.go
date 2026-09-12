package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// serve runs one request through the compression middleware and returns the
// recorded response.
func serve(t *testing.T, acceptEncoding string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/thing", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	CompressionMiddleware(handler).ServeHTTP(rec, req)
	return rec
}

// bigJSON is a body past the compression floor, shaped like the payload this
// middleware exists for: a lot of repeated structure.
func bigJSON() string {
	var b strings.Builder
	b.WriteString(`{"artifacts":[`)
	for i := 0; i < 400; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"type":"requirement","title":"The system shall do the thing","status":"draft"}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

// The whole point: a large JSON body goes down the wire compressed, and comes
// back out byte-identical.
func TestALargeBodyIsCompressedAndStillDecodesToItself(t *testing.T) {
	body := bigJSON()
	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to name Accept-Encoding", vary)
	}
	if rec.Body.Len() >= len(body) {
		t.Errorf("compressed body is %d bytes, no smaller than the %d it started at", rec.Body.Len(), len(body))
	}

	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("the body is not gzip: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("the gzip stream is truncated: %v", err)
	}
	if string(got) != body {
		t.Error("the decompressed body is not what the handler wrote")
	}
}

// An event stream must arrive uncompressed and unbuffered. Compressing one
// does not make it slow, it makes it silently stop working — events sit in
// the compressor's window while the browser waits.
func TestAnEventStreamIsNeverCompressed(t *testing.T) {
	// Long enough to be well past the floor, so nothing but the stream check
	// can be what spares it.
	event := "data: " + strings.Repeat("x", 4000) + "\n\n"

	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, event)
		w.(http.Flusher).Flush()
	})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("an event stream was encoded as %q", got)
	}
	if rec.Body.String() != event {
		t.Error("the event did not reach the client verbatim")
	}
	if !rec.Flushed {
		t.Error("the stream was not flushed through to the client")
	}
}

// A handler that flushes has told us it is streaming, whatever its
// Content-Type says. Buffering it would hold the bytes it just pushed.
func TestFlushingBeforeTheTypeIsKnownStopsCompression(t *testing.T) {
	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "first chunk\n")
		flusher.Flush()
		_, _ = io.WriteString(w, strings.Repeat("later and much larger\n", 500))
	})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("a flushing handler was compressed (%q)", got)
	}
	if !strings.HasPrefix(rec.Body.String(), "first chunk\n") {
		t.Error("the flushed chunk did not arrive first")
	}
}

// Below the floor the gzip header and trailer cost more than they save.
func TestASmallBodyIsLeftAlone(t *testing.T) {
	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("a tiny body was compressed (%q)", got)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// A client that does not offer gzip must never be sent it.
func TestAClientThatDidNotAskGetsPlainBytes(t *testing.T) {
	body := bigJSON()
	for _, accept := range []string{"", "identity", "deflate, br", "gzip;q=0", "gzip;q=0.0"} {
		rec := serve(t, accept, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		})
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("Accept-Encoding %q was answered with %q", accept, got)
		}
		if rec.Body.String() != body {
			t.Errorf("Accept-Encoding %q: the body was altered", accept)
		}
	}
}

// A weight that is present and non-zero is an acceptance, not a refusal.
func TestAWeightedGzipIsStillAccepted(t *testing.T) {
	body := bigJSON()
	for _, accept := range []string{"gzip;q=1.0", "br;q=1.0, gzip;q=0.5", "GZIP"} {
		rec := serve(t, accept, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		})
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Errorf("Accept-Encoding %q was not compressed (%q)", accept, got)
		}
	}
}

// The status the handler chose has to survive being held back, and must not
// be sent twice — net/http logs a warning for the second one.
func TestTheStatusIsPreservedAndSentOnce(t *testing.T) {
	body := bigJSON()
	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, body)
	})
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Error("a 201 with a large body was not compressed")
	}
}

// A body-less status must not claim an encoding.
func TestABodylessStatusCarriesNoEncoding(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		})
		if rec.Code != status {
			t.Errorf("status = %d, want %d", rec.Code, status)
		}
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("status %d carried Content-Encoding %q", status, got)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("status %d grew a body", status)
		}
	}
}

// An error written after nothing else still reaches the client with its
// status, even though it is too small to compress.
func TestAnErrorResponseIsNotSwallowed(t *testing.T) {
	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		writeJSONError(w, http.StatusNotFound, "baseline not found")
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "baseline not found") {
		t.Errorf("the error body was lost: %q", rec.Body.String())
	}
}

// A Content-Length the handler computed describes the uncompressed body; left
// in place beside a gzip encoding it truncates the response.
func TestAStaleContentLengthIsDropped(t *testing.T) {
	body := bigJSON()
	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	})
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length %q survived compression", got)
	}
}

// A body the handler already encoded must not be encoded again.
func TestAnAlreadyEncodedBodyIsLeftAlone(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = io.WriteString(zw, bigJSON())
	_ = zw.Close()
	encoded := buf.Bytes()

	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(encoded)
	})

	if !bytes.Equal(rec.Body.Bytes(), encoded) {
		t.Error("an already-compressed body was compressed a second time")
	}
}
