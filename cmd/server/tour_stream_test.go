//go:build unix

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Event streams in the API tour (refactor plan §6.4 S5b-S5d; invariant I9's
// stream head). A route that answers text/event-stream (internal/api sse.go,
// ServeStream) never ends: it writes its head, replays what the stream holds
// as "event: <name>\ndata: <json>\n\n" frames, flushes once, and then waits
// for live events and a keepalive comment every 25 s until the client
// leaves. Read to its end, as tour.exchange reads every other answer, it
// would hold the tour until the request's 60 s bound. So a request built
// with eventStream(n) is read frame by frame instead: once its answer is an
// event stream, the tour reads until it holds n frames (blocks ending in a
// blank line) and then until tourStreamQuiet passes with no further byte, and
// closes the connection; the server's handler then sees the client leave
// and returns, and /metrics counts it (checkMetrics waits for that). The
// golden shows the bytes read, as text lines, so an extra frame the server
// wrote with its replay, before its one flush, shows too. An answer that is
// not an event stream (a 404 of a phantom session, a rate limit's 429) is
// read to its end as usual.
//
// A stream is read once (no gzip variant; the compressor never compresses
// text/event-stream, S4a's pin), and a step records a note saying how it
// was read. The option works on setup and probe requests too, so an area
// can drain a stream route's rate-limit bucket with probes. Stay far below
// the 25 s keepalive; a frame the server sends live, in answer to another
// request, is not something one request can wait for, so a live broadcast
// is not recorded this way.

// tourStreamWithin bounds the wait for the frames an area expects.
const tourStreamWithin = 10 * time.Second

// tourStreamQuiet is how long the tour keeps reading once it holds the
// frames it expects, for any the server sent beyond them.
const tourStreamQuiet = 300 * time.Millisecond

// tourStream is how an event stream is read: the frames the area expects.
type tourStream struct {
	frames int
}

// eventStream reads the answer, when it is an event stream, frame by frame:
// frames is how many the area expects the replay to hold (0: the head
// alone). The request is sent once, and a step notes how it was read.
func eventStream(frames int) tourOpt {
	return func(tr *tour, r *tourReq) {
		if frames < 0 {
			tr.t.Fatalf("eventStream takes the number of frames the stream replays, not %d", frames)
		}
		r.stream = &tourStream{frames: frames}
		if r.once == "" {
			r.once = "an event stream, read once and then closed by the tour"
		}
		what := fmt.Sprintf("%d frames", frames)
		if frames == 1 {
			what = "1 frame"
		}
		r.notes = append(r.notes, fmt.Sprintf("read as an event stream: the tour reads %s, then on until %s pass "+
			"with no further byte, and closes the connection (an answer that is not an event stream is read to its end)",
			what, tourStreamQuiet))
	}
}

// isEventStream reports whether an answer is an event stream.
func isEventStream(h http.Header) bool {
	mediaType, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
	return mediaType == "text/event-stream"
}

// readStreamAnswer reads an event stream as eventStream asked, then cancels
// the request, which closes the connection. A stream that sent fewer frames
// than the area expects fails the area, and the golden shows what it sent.
func (tr *tour) readStreamAnswer(r *tourReq, body io.Reader, cancel context.CancelFunc) []byte {
	tr.t.Helper()
	data, err := readStream(body, r.stream.frames, tourStreamWithin, tourStreamQuiet)
	cancel()
	if err != nil {
		tr.t.Errorf("%s %s: an event stream the area expected %d frame(s) of: %v (the golden shows what it sent)",
			r.method, r.path, r.stream.frames, err)
	}
	return data
}

// streamFrames counts the frames of an event stream's text: blocks that
// end in a blank line (a frame's data is JSON, which holds no raw newline;
// a keepalive comment is a frame too).
func streamFrames(b []byte) int { return bytes.Count(b, []byte("\n\n")) }

// readStream reads body until it holds frames frames, within the given
// time, and then until quiet passes with no further byte. It returns what
// it read, and an error when the frames did not come within the time or
// the stream ended or failed before them. The reading goroutine stops when
// the caller closes the body.
func readStream(body io.Reader, frames int, within, quiet time.Duration) ([]byte, error) {
	type chunk struct {
		data []byte
		err  error
	}
	chunks := make(chan chunk)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			buf := make([]byte, 4096)
			n, err := body.Read(buf)
			select {
			case chunks <- chunk{buf[:n], err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	var got []byte
	ended := func(err error, what string) error {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("the stream ended %s", what)
		}
		return fmt.Errorf("the stream failed %s: %v", what, err)
	}
	deadline := time.NewTimer(within)
	defer deadline.Stop()
	for streamFrames(got) < frames {
		select {
		case c := <-chunks:
			got = append(got, c.data...)
			if c.err != nil && streamFrames(got) < frames {
				return got, ended(c.err, fmt.Sprintf("after %d of %d frames", streamFrames(got), frames))
			}
			if c.err != nil {
				return got, nil
			}
		case <-deadline.C:
			return got, fmt.Errorf("it sent %d of %d frames within %s", streamFrames(got), frames, within)
		}
	}
	silence := time.NewTimer(quiet)
	defer silence.Stop()
	for {
		select {
		case c := <-chunks:
			got = append(got, c.data...)
			if errors.Is(c.err, io.EOF) {
				return got, nil // the server ended it: everything it sent was read
			}
			if c.err != nil {
				return got, ended(c.err, "after the frames expected")
			}
			silence.Reset(quiet)
		case <-silence.C:
			return got, nil
		}
	}
}

// TestTourStream checks, with no database, how the tour reads an event
// stream: readStream's frames, its wait for more and its failures, then a
// step against a server that answers as ServeStream does (the head, a
// replay, one flush, then nothing until the client leaves): the step ends,
// the handler sees the client leave, the request is counted under its
// route, and the record holds the frames as text with no gzip variant; an
// answer that is not an event stream is read to its end.
func TestTourStream(t *testing.T) {
	frame := func(n int) string { return fmt.Sprintf("event: message\ndata: {\"n\":%d}\n\n", n) }
	// feed writes the parts to a pipe, pausing before each, and keeps the
	// pipe open (a stream that never ends) unless end is set.
	feed := func(end bool, parts ...string) (*io.PipeReader, func()) {
		pr, pw := io.Pipe()
		go func() {
			for _, p := range parts {
				time.Sleep(20 * time.Millisecond)
				if _, err := pw.Write([]byte(p)); err != nil {
					return
				}
			}
			if end {
				_ = pw.Close()
			}
		}()
		return pr, func() { _ = pw.Close(); _ = pr.Close() }
	}
	for _, c := range []struct {
		name   string
		end    bool
		parts  []string
		frames int
		want   string
		err    string
	}{
		{name: "exactly the frames expected", parts: []string{frame(0), frame(1)}, frames: 2, want: frame(0) + frame(1)},
		{name: "a frame split across writes", parts: []string{"event: message\nda", "ta: {\"n\":0}\n", "\n"}, frames: 1,
			want: frame(0)},
		{name: "a frame beyond those expected, within the quiet time", parts: []string{frame(0), frame(1), frame(2)},
			frames: 2, want: frame(0) + frame(1) + frame(2)},
		{name: "no frame: the head alone", frames: 0, want: ""},
		{name: "fewer frames than expected", parts: []string{frame(0)}, frames: 2, want: frame(0),
			err: "it sent 1 of 2 frames within 500ms"},
		{name: "a stream that ends before the frames", end: true, parts: []string{frame(0)}, frames: 2, want: frame(0),
			err: "the stream ended after 1 of 2 frames"},
		{name: "a stream that ends after them", end: true, parts: []string{frame(0)}, frames: 1, want: frame(0)},
	} {
		body, stop := feed(c.end, c.parts...)
		start := time.Now()
		got, err := readStream(body, c.frames, 500*time.Millisecond, 100*time.Millisecond)
		took := time.Since(start)
		stop()
		switch {
		case string(got) != c.want:
			t.Errorf("%s: read %q, want %q", c.name, got, c.want)
		case c.err == "" && err != nil, c.err != "" && (err == nil || err.Error() != c.err):
			t.Errorf("%s: error %v, want %q", c.name, err, c.err)
		case took > 2*time.Second:
			t.Errorf("%s: took %s", c.name, took)
		}
	}

	// A step against a server that streams as ServeStream does.
	left := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/missing/") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"guided session not found"}`+"\n")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		_, _ = io.WriteString(w, frame(0)+frame(1))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		left <- r.URL.Path
	}))
	defer srv.Close()
	tr := &tour{t: t, s: &serverProcess{base: srv.URL}, norm: newTourNormaliser(), names: map[string]string{},
		values: map[string]string{}, sent: map[string]int{}, seen: map[string]bool{}, whole: map[string]string{}}
	tr.loadRoutes()
	const route = "GET /api/v1/guided-sessions/{id}/chat/stream"
	viewer := &tourActor{name: "viewer", session: "viewer-session", org: "w", home: "w"}
	start := time.Now()
	res := tr.step("the stream", viewer, route, at("id", "live"), eventStream(2))
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the stream step took %s", took)
	}
	if string(res.body) != frame(0)+frame(1) {
		t.Errorf("the stream step read %q", res.body)
	}
	select {
	case p := <-left:
		if p != "/api/v1/guided-sessions/live/chat/stream" {
			t.Errorf("the handler that saw the client leave served %s", p)
		}
	case <-time.After(2 * time.Second):
		t.Error("the stream's handler did not see the client leave")
	}
	rec := tr.renderStep(tr.steps[0])
	if rec.ContentType == nil || *rec.ContentType != "text/event-stream" || rec.ContentLength != nil ||
		rec.Kind != "text" || strings.Join(rec.Lines, "|") != `event: message|data: {"n":0}||event: message|data: {"n":1}|` ||
		rec.Gzip != "not sent: an event stream, read once and then closed by the tour" || len(rec.Notes) != 1 ||
		!strings.Contains(rec.Headers[0], "Cache-Control: no-cache") {
		t.Errorf("the stream step's record: %+v", rec)
	}

	// A stream whose frame holds a time the server minted: its length varies
	// from run to run, but it never has a Content-Length, and the record
	// says so (null, not "<varies>").
	timed := "event: message\ndata: {\"at\":\"" + time.Now().UTC().Format(time.RFC3339Nano) + "\"}\n\n"
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, timed)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv2.Close()
	tr2 := &tour{t: t, s: &serverProcess{base: srv2.URL}, norm: newTourNormaliser(), names: map[string]string{},
		values: map[string]string{}, sent: map[string]int{}, seen: map[string]bool{}, whole: map[string]string{}}
	tr2.loadRoutes()
	if res := tr2.step("a stream with a time", viewer, route, at("id", "timed"), eventStream(1)); string(res.body) != timed {
		t.Errorf("the timed stream step read %q", res.body)
	}
	if lo, hi := bodyBand(tr2.norm, []byte(timed)); lo == hi {
		t.Errorf("a frame with a time should vary in length (%d to %d)", lo, hi)
	}
	if rec := tr2.renderStep(tr2.steps[0]); rec.ContentLength != nil {
		t.Errorf("the timed stream step's content_length: %v, want null", rec.ContentLength)
	}

	// A probe that drains nothing but must not hang, and an answer that is
	// not an event stream, read to its end.
	if p := tr.probe(viewer, route, at("id", "live"), eventStream(0)); p.status != http.StatusOK || len(p.body) != 2*len(frame(0)) {
		t.Errorf("the stream probe: %d %q", p.status, p.body)
	}
	if res := tr.step("a session that does not exist", viewer, route, at("id", "missing"), eventStream(2)); res.status != http.StatusNotFound ||
		string(res.body) != `{"error":"guided session not found"}`+"\n" {
		t.Errorf("the not-found step: %d %q", res.status, res.body)
	}
	if got := tr.sent; got[route+" 200"] != 2 || got[route+" 404"] != 1 || len(got) != 2 {
		t.Errorf("the requests counted under their route: %v", got)
	}
}
