package client

import (
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultMaxRetries = 8
	defaultBaseDelay  = 500 * time.Millisecond
	defaultMaxDelay   = 30 * time.Second
)

// retryTransport retries transient server responses that Medplum (and the
// gravitee gateway in front of it) return under load: HTTP 429 plus the
// 502/503/504 gateway errors. A Terraform apply fans many resource operations
// out concurrently, so the provider must ride out a throttle rather than fail
// the whole apply on the first 429. When the server sends a Retry-After header
// it is honored (capped); otherwise the wait grows exponentially up to a cap.
//
// It wraps the base transport beneath the oauth2 token layer, so a retried
// request still carries a valid bearer token.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func newRetryTransport(base http.RoundTripper) *retryTransport {
	return &retryTransport{
		base:       base,
		maxRetries: defaultMaxRetries,
		baseDelay:  defaultBaseDelay,
		maxDelay:   defaultMaxDelay,
	}
}

func (t *retryTransport) transport() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.transport()

	for attempt := 0; ; attempt++ {
		// Each retry runs on a fresh clone so the caller's request is never
		// mutated and the body is replayed from GetBody. Requests built by this
		// package use a *bytes.Reader, so net/http populates GetBody; a request
		// that carries a body but no GetBody cannot be replayed.
		attemptReq := req
		if attempt > 0 {
			attemptReq = req.Clone(req.Context())
			if req.Body != nil {
				if req.GetBody == nil {
					return base.RoundTrip(req)
				}
				body, err := req.GetBody()
				if err != nil {
					return nil, err
				}
				attemptReq.Body = body
			}
		}

		resp, err := base.RoundTrip(attemptReq)
		if err != nil {
			return nil, err
		}
		if attempt >= t.maxRetries || !retryableStatus(resp.StatusCode) {
			return resp, nil
		}

		delay := t.delay(attempt, resp.Header.Get("Retry-After"))

		// Drain and close so the connection can be reused before we wait.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
		}
	}
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}

// delay returns the wait before the next attempt. A valid Retry-After header
// (delta-seconds or HTTP-date) wins, capped at maxDelay; otherwise the wait is
// baseDelay * 2^attempt, also capped at maxDelay.
func (t *retryTransport) delay(attempt int, retryAfter string) time.Duration {
	if d, ok := parseRetryAfter(retryAfter); ok {
		switch {
		case d < 0:
			return 0
		case d > t.maxDelay:
			return t.maxDelay
		default:
			return d
		}
	}
	d := t.baseDelay << attempt
	if d <= 0 || d > t.maxDelay {
		return t.maxDelay
	}
	return d
}

// parseRetryAfter parses an HTTP Retry-After value, which is either an integer
// number of seconds or an HTTP-date.
func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second, true
	}
	if when, err := http.ParseTime(v); err == nil {
		return time.Until(when), true
	}
	return 0, false
}
