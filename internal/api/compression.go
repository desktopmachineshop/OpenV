package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Response compression.
//
// The API ships some genuinely large JSON. A baseline snapshot is a whole
// project export — the OpenV Platform project is 942 KB of pretty-printed
// JSON — and until this middleware existed every byte of it went down the
// wire uncompressed. On a phone that is several seconds of nothing happening,
// which reads as a broken button rather than a slow one. Gzip takes that
// particular payload to about 193 KB.
//
// TWO RULES MATTER MORE THAN THE COMPRESSION ITSELF.
//
// First, a Server-Sent Events stream must never be compressed or buffered.
// The API holds SSE connections open for minutes (notifications, guided chat,
// agent run logs) and every event has to reach the browser the moment it is
// written. A gzip writer batches until its window fills, so compressing a
// stream does not slow it down — it silently breaks it, and nothing surfaces
// until somebody notices their notifications stopped arriving. The decision
// is therefore made from the Content-Type the handler actually set, on the
// first write, rather than from a list of paths somebody has to remember to
// update when a new stream is added.
//
// Second, http.Flusher has to survive the wrapping. A handler that streams
// calls Flush to push bytes out; if the wrapper does not implement Flusher,
// the type assertion those handlers make fails and they either buffer
// forever or panic. So the wrapper implements it, flushing the gzip writer
// before the underlying one.

// compressMinBytes is the smallest response worth compressing. Below roughly
// this size the gzip header and trailer cost more than the compression saves,
// and the CPU is spent for nothing. Most API responses are small; the ones
// this middleware exists for are two orders of magnitude above the floor.
const compressMinBytes = 1400

// gzipWriterPool reuses the compressor's allocations, which are substantial
// (a gzip.Writer carries a 32 KB window). Without pooling, a burst of
// requests allocates one per response.
var gzipWriterPool = sync.Pool{
	New: func() interface{} { return gzip.NewWriter(io.Discard) },
}

// CompressionMiddleware gzips responses for clients that accept it, leaving
// event streams and small payloads alone.
func CompressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		// A handler that has already encoded its body (or a proxied response
		// that arrives encoded) must not be encoded twice.
		cw := &compressingWriter{ResponseWriter: w}
		defer cw.Close()
		next.ServeHTTP(cw, r)
	})
}

// acceptsGzip reports whether the client offered gzip. The header is a list
// of codings with optional quality values; an explicit "gzip;q=0" is a
// refusal, and is the one case worth parsing for rather than substring
// matching, because honouring it wrongly produces a body the client will not
// decode.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			continue
		}
		for _, param := range fields[1:] {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(param, "q=") && isZeroQuality(param[2:]) {
				return false
			}
		}
		return true
	}
	return false
}

// isZeroQuality reports whether a q-value means "do not send me this".
// An unparseable value is not treated as a refusal: the client asked for gzip
// and only the weight is malformed.
func isZeroQuality(q string) bool {
	weight, err := strconv.ParseFloat(strings.TrimSpace(q), 64)
	return err == nil && weight == 0
}

// compressingWriter decides on the first write whether this response should
// be gzipped, then behaves as either a compressing or a transparent writer
// for the rest of its life.
type compressingWriter struct {
	http.ResponseWriter

	gz *gzip.Writer
	// decided records that the compress-or-not choice has been made, so it
	// is made exactly once per response.
	decided bool
	// buffered holds the first bytes written while the response is still too
	// small to judge. A short response never reaches the threshold and is
	// released uncompressed when the handler returns.
	buffered []byte
	// passthrough marks a response that must not be compressed: a stream, an
	// already-encoded body, or a status with no body at all.
	passthrough bool
	// wroteHeader records that the handler set a status; statusSent records
	// that we have forwarded it. They are separate because the status is held
	// back until the Content-Encoding decision is made, and forwarding it
	// twice would make net/http log a superfluous-WriteHeader warning on
	// every empty response.
	wroteHeader bool
	statusSent  bool
	status      int
}

func (c *compressingWriter) WriteHeader(status int) {
	if c.wroteHeader {
		return
	}
	c.status = status
	c.wroteHeader = true
	// 204 and 304 carry no body, and a Content-Encoding on them is a protocol
	// error rather than a wasted header.
	if status == http.StatusNoContent || status == http.StatusNotModified {
		c.passthrough, c.decided = true, true
		c.flushStatus()
	}
	// Otherwise the status is held until the first write, because the
	// Content-Encoding header must be set before it goes out and we do not
	// yet know whether there will be one.
}

func (c *compressingWriter) Write(p []byte) (int, error) {
	if !c.decided {
		if c.shouldPassThrough() {
			c.startPassthrough()
		} else if len(c.buffered)+len(p) < compressMinBytes {
			// Too small to judge yet: hold it. A handler that writes its
			// whole body in one call decides immediately; one that dribbles
			// accumulates until it crosses the floor or finishes.
			c.buffered = append(c.buffered, p...)
			return len(p), nil
		} else {
			c.startCompressing()
		}
	}
	if c.gz != nil {
		return c.gz.Write(p)
	}
	return c.ResponseWriter.Write(p)
}

// shouldPassThrough reads the headers the handler set to decide whether this
// response can be compressed at all.
func (c *compressingWriter) shouldPassThrough() bool {
	h := c.Header()
	// An event stream: compressing it would hold events in the compressor's
	// window instead of delivering them.
	if isEventStream(h.Get("Content-Type")) {
		return true
	}
	// Already encoded by the handler, or deliberately marked identity.
	if enc := h.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return true
	}
	return false
}

func (c *compressingWriter) startPassthrough() {
	c.passthrough, c.decided = true, true
	c.flushStatus()
	c.releaseBuffered()
}

func (c *compressingWriter) startCompressing() {
	c.decided = true
	h := c.Header()
	h.Set("Content-Encoding", "gzip")
	// The body length changes, so any length the handler computed is now
	// wrong; leaving it would truncate the response.
	h.Del("Content-Length")
	// Caches and proxies must keep the encodings apart.
	appendVary(h, "Accept-Encoding")
	c.flushStatus()

	gz := gzipWriterPool.Get().(*gzip.Writer)
	gz.Reset(c.ResponseWriter)
	c.gz = gz
	if len(c.buffered) > 0 {
		_, _ = c.gz.Write(c.buffered)
		c.buffered = nil
	}
}

// flushStatus sends the status line that WriteHeader deferred.
func (c *compressingWriter) flushStatus() {
	if c.wroteHeader && !c.statusSent {
		c.statusSent = true
		c.ResponseWriter.WriteHeader(c.status)
	}
}

func (c *compressingWriter) releaseBuffered() {
	if len(c.buffered) > 0 {
		_, _ = c.ResponseWriter.Write(c.buffered)
		c.buffered = nil
	}
}

// Flush keeps streaming handlers working. An undecided response that is being
// flushed is a handler pushing bytes out now, which is the signature of a
// stream even when the Content-Type did not say so, so it stops being a
// compression candidate.
func (c *compressingWriter) Flush() {
	if !c.decided {
		c.startPassthrough()
	}
	if c.gz != nil {
		_ = c.gz.Flush()
	}
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Close finishes the response: a compressed one gets its gzip trailer, and a
// response too small to compress is released as it was written.
func (c *compressingWriter) Close() {
	if !c.decided {
		c.startPassthrough()
	}
	if c.gz != nil {
		_ = c.gz.Close()
		gzipWriterPool.Put(c.gz)
		c.gz = nil
		return
	}
	c.releaseBuffered()
	c.flushStatus()
}

// isEventStream reports whether a Content-Type names Server-Sent Events.
func isEventStream(contentType string) bool {
	mediaType := contentType
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = mediaType[:i]
	}
	return strings.EqualFold(strings.TrimSpace(mediaType), "text/event-stream")
}

// appendVary adds a field to Vary without duplicating one already there.
func appendVary(h http.Header, field string) {
	for _, existing := range h.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), field) {
				return
			}
		}
	}
	h.Add("Vary", field)
}
