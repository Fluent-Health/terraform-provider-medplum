package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetryTransport mirrors the production transport but with millisecond
// delays so tests don't sleep for seconds.
func fastRetryTransport() *retryTransport {
	return &retryTransport{
		base:       http.DefaultTransport,
		maxRetries: 5,
		baseDelay:  time.Millisecond,
		maxDelay:   5 * time.Millisecond,
	}
}

func TestRetryTransport_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := &http.Client{Transport: fastRetryTransport()}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("calls = %d, want 3 (two 429s then success)", got)
	}
}

func TestRetryTransport_GivesUpAfterMaxRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rt := fastRetryTransport()
	c := &http.Client{Transport: rt}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if want := int32(rt.maxRetries + 1); atomic.LoadInt32(&calls) != want {
		t.Fatalf("calls = %d, want %d (initial + maxRetries)", calls, want)
	}
}

func TestRetryTransport_DoesNotRetryNonRetryable4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := &http.Client{Transport: fastRetryTransport()}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want 1 (400 must not be retried)", got)
	}
}

func TestRetryTransport_ReplaysRequestBody(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []string
		calls  int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		if atomic.AddInt32(&calls, 1) < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &http.Client{Transport: fastRetryTransport()}
	resp, err := c.Post(srv.URL, "application/json", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("got %d request bodies, want 2", len(bodies))
	}
	for i, b := range bodies {
		if b != `{"a":1}` {
			t.Fatalf("body[%d] = %q, want the original payload on every attempt", i, b)
		}
	}
}

func TestRetryTransport_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	rt := &retryTransport{base: http.DefaultTransport, maxRetries: 5, baseDelay: 50 * time.Millisecond, maxDelay: time.Second}
	c := &http.Client{Transport: rt}

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	if _, err := c.Do(req); err == nil {
		t.Fatal("expected an error once the context is canceled mid-backoff")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d, ok := parseRetryAfter("3"); !ok || d != 3*time.Second {
		t.Fatalf("parseRetryAfter(\"3\") = %v, %v; want 3s, true", d, ok)
	}
	if _, ok := parseRetryAfter(""); ok {
		t.Fatal("parseRetryAfter(\"\") should report ok=false")
	}
	if _, ok := parseRetryAfter("not-a-number"); ok {
		t.Fatal("parseRetryAfter(garbage) should report ok=false")
	}
	// HTTP-date form.
	future := time.Now().UTC().Add(2 * time.Second).Format(http.TimeFormat)
	if d, ok := parseRetryAfter(future); !ok || d <= 0 {
		t.Fatalf("parseRetryAfter(date) = %v, %v; want a positive duration, true", d, ok)
	}
}

func TestRetryTransport_Delay(t *testing.T) {
	rt := &retryTransport{maxRetries: 8, baseDelay: time.Second, maxDelay: 30 * time.Second}

	// Retry-After in seconds wins, capped at maxDelay.
	if got := rt.delay(0, "100"); got != 30*time.Second {
		t.Fatalf("delay with Retry-After 100s = %v, want capped 30s", got)
	}
	// No header: exponential baseDelay * 2^attempt.
	if got := rt.delay(2, ""); got != 4*time.Second {
		t.Fatalf("delay(attempt=2) = %v, want 4s", got)
	}
	// Exponential growth capped at maxDelay.
	if got := rt.delay(20, ""); got != 30*time.Second {
		t.Fatalf("delay(attempt=20) = %v, want capped 30s", got)
	}
}
