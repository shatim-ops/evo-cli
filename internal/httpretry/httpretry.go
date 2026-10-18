// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package httpretry is an http.RoundTripper that repeats requests Cloud.ru
// did not apply: a dropped connection on a read, 429, and 502/503/504 on a
// read. The compute adapter keeps its own loop (internal/provision); the
// other service clients get this transport.
package httpretry

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/shatim-ops/evo-cli/internal/product"
)

const (
	// MaxRetries is the number of repeats after the first attempt.
	MaxRetries = 3
	// maxRetryAfter caps a server-sent Retry-After.
	maxRetryAfter = 30 * time.Second
)

// Transport repeats a request on:
//   - a transport error (connection reset, EOF) — GET and HEAD only: a POST,
//     PUT, PATCH or DELETE may have reached the server, and a repeated DELETE
//     answers 404, which the service clients report as an error;
//   - 429 — any method whose body can be replayed: the server refused it;
//   - 502, 503, 504 — GET and HEAD only.
//
// Waits double from Backoff (1 s by default); a Retry-After of up to 30 s
// replaces the wait. The request context stops the waits.
type Transport struct {
	Base    http.RoundTripper // nil: http.DefaultTransport
	Backoff time.Duration     // first wait; 0: 1 s
}

// NewClient returns an http.Client with this transport. timeout covers all
// attempts together, as http.Client.Timeout does.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &Transport{}}
}

func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func readMethod(m string) bool { return m == http.MethodGet || m == http.MethodHead }

// UserAgent wraps base (nil: http.DefaultTransport) so that a request
// without a User-Agent carries evo's (product.UserAgent). For the clients
// that keep their own retry loop: the compute adapter and IAM.
func UserAgent(base http.RoundTripper) http.RoundTripper { return uaTransport{base} }

type uaTransport struct{ base http.RoundTripper }

func (t uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(withUserAgent(req))
}

// withUserAgent returns req with evo's User-Agent, or req itself when it
// already has one. A RoundTripper must not change the caller's request.
func withUserAgent(req *http.Request) *http.Request {
	if req.Header.Get("User-Agent") != "" {
		return req
	}
	r := req.Clone(req.Context())
	r.Header.Set("User-Agent", product.UserAgent())
	return r
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = withUserAgent(req)
	backoff := t.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	replayable := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	for attempt := 0; ; attempt++ {
		r := req
		if attempt > 0 && req.Body != nil && req.Body != http.NoBody {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			r = req.Clone(req.Context())
			r.Body = body
		}
		wait := backoff
		resp, err := t.base().RoundTrip(r)
		last := attempt == MaxRetries || req.Context().Err() != nil
		if err != nil {
			if last || !readMethod(req.Method) || !replayable {
				return nil, err
			}
		} else {
			retry := replayable && (resp.StatusCode == http.StatusTooManyRequests ||
				readMethod(req.Method) && (resp.StatusCode == http.StatusBadGateway ||
					resp.StatusCode == http.StatusServiceUnavailable ||
					resp.StatusCode == http.StatusGatewayTimeout))
			if !retry || last {
				return resp, nil
			}
			if d, ok := retryAfter(resp.Header.Get("Retry-After")); ok {
				wait = d
			}
			// Drain a little so the connection can be reused.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
		}
		timer := time.NewTimer(wait)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
		backoff *= 2
	}
}

// retryAfter parses Retry-After in seconds; dates are ignored.
func retryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	d := time.Duration(n) * time.Second
	if d > maxRetryAfter {
		d = maxRetryAfter
	}
	return d, true
}
