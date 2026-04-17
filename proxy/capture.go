package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AgusRdz/probe/observer"
)

// captureResponseWriter wraps http.ResponseWriter to intercept the response body
// while still streaming it to the client unchanged.
type captureResponseWriter struct {
	http.ResponseWriter
	buf        *bytes.Buffer
	statusCode int
}

func (crw *captureResponseWriter) WriteHeader(code int) {
	crw.statusCode = code
	crw.ResponseWriter.WriteHeader(code)
}

func (crw *captureResponseWriter) Write(b []byte) (int, error) {
	crw.buf.Write(b)
	return crw.ResponseWriter.Write(b)
}

// wrapCapture wraps the handler to:
//  1. Read the request body via io.TeeReader — original flows to target, copy goes to buf
//  2. Capture response via captureResponseWriter
//  3. Build a CapturedPair and send it to the channel (non-blocking: if channel full, drop)
//  4. Replace r.Body with a new io.NopCloser from the tee'd buffer so the proxy can read it
//
// Body cap: read up to bodyLimit bytes from req/resp; discard the rest (but still forward it).
func wrapCapture(h http.Handler, ch chan<- observer.CapturedPair, bodyLimit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// --- request body capture ---
		var reqBuf bytes.Buffer
		if r.Body != nil {
			// TeeReader: reads from r.Body and writes to reqBuf simultaneously.
			// We cap the tee at bodyLimit; the remainder still flows to the target
			// via the original r.Body wrapped in MultiReader.
			limitedBody := io.LimitReader(r.Body, bodyLimit)
			tee := io.TeeReader(limitedBody, &reqBuf)

			// Drain the tee into a discard sink to fill reqBuf up to bodyLimit,
			// then reconstruct the full body for the proxy using MultiReader.
			var capBuf bytes.Buffer
			io.Copy(&capBuf, tee) //nolint:errcheck

			// r.Body still has data past bodyLimit (if any). Reassemble full body:
			// capBuf (already read portion) + remainder of original r.Body.
			r.Body = io.NopCloser(io.MultiReader(&capBuf, r.Body))
		}

		// --- response capture ---
		crw := &captureResponseWriter{
			ResponseWriter: w,
			buf:            &bytes.Buffer{},
			statusCode:     http.StatusOK,
		}

		h.ServeHTTP(crw, r)

		latency := time.Since(start).Milliseconds()

		// Cap response body at bodyLimit.
		respBytes := crw.buf.Bytes()
		if int64(len(respBytes)) > bodyLimit {
			respBytes = respBytes[:bodyLimit]
		}

		// Cap captured request body too (already limited by LimitReader, but be safe).
		reqBytes := reqBuf.Bytes()
		if int64(len(reqBytes)) > bodyLimit {
			reqBytes = reqBytes[:bodyLimit]
		}

		pair := observer.CapturedPair{
			Method:          r.Method,
			RawPath:         r.URL.RequestURI(),
			ReqContentType:  r.Header.Get("Content-Type"),
			RespContentType: crw.Header().Get("Content-Type"),
			StatusCode:      crw.statusCode,
			LatencyMs:       latency,
			ReqBody:         reqBytes,
			RespBody:        respBytes,
			ReqHeaders:      captureRequestHeaders(r.Header),
		}

		// Non-blocking send: drop observation rather than stall the proxy.
		select {
		case ch <- pair:
		default:
			fmt.Fprintf(os.Stderr, "probe: capture channel full — dropping observation for %s %s\n",
				r.Method, r.URL.Path)
		}
	})
}

// headersToSkip lists request headers that are too noisy or internal to be
// useful in a Postman collection. These are never tracked.
var headersToSkip = map[string]bool{
	"accept-encoding":     true,
	"connection":          true,
	"content-length":      true,
	"host":                true,
	"keep-alive":          true,
	"origin":              true,
	"proxy-authorization": true,
	"referer":             true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"user-agent":          true,
}

// captureRequestHeaders returns the canonical names of request headers worth
// tracking for documentation purposes. Values are never captured.
// Skips browser-internal, hop-by-hop, and cookie headers.
func captureRequestHeaders(h http.Header) []string {
	var names []string
	for name := range h {
		lower := strings.ToLower(name)
		if lower == "cookie" || strings.HasPrefix(lower, "sec-") {
			continue
		}
		if headersToSkip[lower] {
			continue
		}
		names = append(names, http.CanonicalHeaderKey(name))
	}
	return names
}
