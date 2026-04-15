package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AgusRdz/probe/observer"
)

// TestCaptureResponseWriter verifies that WriteHeader stores the status code and
// Write buffers the body while still delegating to the underlying ResponseWriter.
func TestCaptureResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	crw := &captureResponseWriter{
		ResponseWriter: rec,
		buf:            &bytes.Buffer{},
		statusCode:     http.StatusOK,
	}

	crw.WriteHeader(http.StatusCreated)
	if crw.statusCode != http.StatusCreated {
		t.Fatalf("statusCode: got %d, want %d", crw.statusCode, http.StatusCreated)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("underlying recorder code: got %d, want %d", rec.Code, http.StatusCreated)
	}

	payload := []byte("hello proxy")
	n, err := crw.Write(payload)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write n: got %d, want %d", n, len(payload))
	}
	if crw.buf.String() != string(payload) {
		t.Fatalf("buf: got %q, want %q", crw.buf.String(), string(payload))
	}
	if rec.Body.String() != string(payload) {
		t.Fatalf("underlying recorder body: got %q, want %q", rec.Body.String(), string(payload))
	}
}

// TestWrapCapture_SendsToPair checks that a CapturedPair is placed on the channel
// with the correct Method, RawPath, StatusCode, and RespBody.
func TestWrapCapture_SendsToPair(t *testing.T) {
	// Stand up a real httptest.Server acting as the "target".
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer target.Close()

	ch := make(chan observer.CapturedPair, 1)

	// inner simulates a reverse proxy: forward the request to the target server.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(target.URL + r.URL.RequestURI())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		w.Write(body) //nolint:errcheck
	})

	handler := wrapCapture(inner, ch, 1<<20)

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	select {
	case pair := <-ch:
		if pair.Method != http.MethodGet {
			t.Errorf("Method: got %q, want %q", pair.Method, http.MethodGet)
		}
		if pair.RawPath != "/api/users" {
			t.Errorf("RawPath: got %q, want %q", pair.RawPath, "/api/users")
		}
		if pair.StatusCode != http.StatusAccepted {
			t.Errorf("StatusCode: got %d, want %d", pair.StatusCode, http.StatusAccepted)
		}
		if !bytes.Contains(pair.RespBody, []byte(`"ok":true`)) {
			t.Errorf("RespBody: got %q, want it to contain %q", pair.RespBody, `"ok":true`)
		}
	default:
		t.Fatal("expected CapturedPair on channel, got nothing")
	}
}

// TestWrapCapture_BodyNotMutated verifies that the full request body reaches the
// inner handler even after capture has read its copy.
func TestWrapCapture_BodyNotMutated(t *testing.T) {
	const body = `{"name":"alice"}`

	var received string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		received = string(b)
		w.WriteHeader(http.StatusOK)
	})

	ch := make(chan observer.CapturedPair, 1)
	handler := wrapCapture(inner, ch, 1<<20)

	req := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if received != body {
		t.Fatalf("inner handler body: got %q, want %q", received, body)
	}

	select {
	case pair := <-ch:
		if string(pair.ReqBody) != body {
			t.Errorf("CapturedPair.ReqBody: got %q, want %q", pair.ReqBody, body)
		}
	default:
		t.Fatal("expected CapturedPair on channel")
	}
}

// TestWrapCapture_ChannelFull_NoPanic fills the channel to capacity then fires
// one more request — the handler must not block or panic.
func TestWrapCapture_ChannelFull_NoPanic(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	const cap = 2
	ch := make(chan observer.CapturedPair, cap)

	// Pre-fill the channel.
	for i := 0; i < cap; i++ {
		ch <- observer.CapturedPair{}
	}

	handler := wrapCapture(inner, ch, 1<<20)

	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	select {
	case <-done:
		// Pass — handler returned without blocking.
	case <-time.After(2 * time.Second):
		t.Fatal("handler blocked when channel was full")
	}
}
