package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/observer"
	"github.com/AgusRdz/probe/store"
)

const channelBuffer = 100

// Proxy is a transparent reverse proxy that captures traffic for schema inference.
type Proxy struct {
	target *url.URL
	rp     *httputil.ReverseProxy
	ch     chan observer.CapturedPair
	store  *store.Store
	config *config.Config
	done   chan struct{}
}

// New creates and validates a new Proxy.
// Validates target: must be http:// or https://, must respond to HEAD request.
// Starts the drainer goroutine (reads from ch, calls observer.Extract, calls store.Record).
// Strips X-Forwarded-Host and X-Real-IP from forwarded requests (security invariant 5).
// Default channel buffer size: 100.
// When insecure is true, TLS certificate verification is skipped (for dev environments
// with self-signed certs, IIS Express, mkcert, etc.).
func New(target string, s *store.Store, cfg *config.Config, insecure bool) (*Proxy, error) {
	// Validate scheme first.
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return nil, fmt.Errorf("probe: target must begin with http:// or https://: %s", target)
	}

	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("probe: invalid target URL %q: %w", target, err)
	}

	// Shared transport — used by both the startup probe and the reverse proxy.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec — explicit user opt-in
	}

	// Validate reachability with a HEAD request (5s timeout).
	client := &http.Client{Timeout: 5 * time.Second, Transport: transport}
	resp, err := client.Head(target)
	if err != nil {
		return nil, fmt.Errorf("probe: target unreachable: %s", target)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("probe: target unreachable: %s", target)
	}
	// 4xx is fine — target is up, just requires auth.

	rp := httputil.NewSingleHostReverseProxy(targetURL)
	rp.Transport = transport

	// Capture the default director so we can run it then strip sensitive headers.
	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalDirector(req)
		// Security invariant 5: strip headers that could leak internal topology.
		req.Header.Del("X-Forwarded-Host")
		req.Header.Del("X-Real-IP")
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		fmt.Fprintf(os.Stderr, "probe: reverse proxy error for %s %s: %v\n",
			r.Method, r.URL.Path, err)
		w.WriteHeader(http.StatusBadGateway)
	}

	ch := make(chan observer.CapturedPair, channelBuffer)
	done := make(chan struct{})

	p := &Proxy{
		target: targetURL,
		rp:     rp,
		ch:     ch,
		store:  s,
		config: cfg,
		done:   done,
	}

	// Drainer goroutine: async DB writes, decoupled from proxy latency.
	go func() {
		for pair := range p.ch {
			reqSchema, respSchema := observer.Extract(pair)
			if err := p.store.Record(pair, reqSchema, respSchema); err != nil {
				fmt.Fprintf(os.Stderr, "probe: store error: %v\n", err)
			}
		}
		close(p.done)
	}()

	return p, nil
}

// Handler returns the http.Handler to pass to http.ListenAndServe.
// Applies wrapCapture middleware with filter/ignore path logic from config.
func (p *Proxy) Handler(filter, ignore string) http.Handler {
	bodyLimit := int64(p.config.Proxy.BodySizeLimit)
	if bodyLimit <= 0 {
		bodyLimit = 1 << 20 // 1MB fallback
	}

	// Build ignore prefix list from comma-separated string.
	var ignorePrefixes []string
	for _, seg := range strings.Split(ignore, ",") {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			ignorePrefixes = append(ignorePrefixes, seg)
		}
	}

	// Inner handler: the reverse proxy itself.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.rp.ServeHTTP(w, r)
	})

	// Outer handler: decides whether to capture or just proxy.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		shouldCapture := true

		// filter: only capture paths that start with filter (when filter is set).
		if filter != "" && !strings.HasPrefix(path, filter) {
			shouldCapture = false
		}

		// ignore: skip capture for any matching prefix.
		if shouldCapture {
			for _, prefix := range ignorePrefixes {
				if strings.HasPrefix(path, prefix) {
					shouldCapture = false
					break
				}
			}
		}

		if shouldCapture {
			wrapCapture(inner, p.ch, bodyLimit).ServeHTTP(w, r)
		} else {
			// Traffic still proxied — just not captured.
			inner.ServeHTTP(w, r)
		}
	})
}

// Shutdown gracefully drains the channel and stops the drainer goroutine.
// Blocks until the drainer has processed all buffered pairs or ctx is cancelled.
func (p *Proxy) Shutdown(ctx context.Context) error {
	close(p.ch)
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
