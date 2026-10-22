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
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/product"
)

const (
	// MaxRetries is the number of repeats after the first attempt.
	MaxRetries = 3
	// maxRetryAfter caps a server-sent Retry-After.
	maxRetryAfter = 30 * time.Second
)

// Transport repeats a request on:
//   - a transport error (connection reset, EOF; not a certificate the system
//     does not trust) — GET and HEAD only: a POST,
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
	Policy  Policy            // what is repeated besides the above
}

// Policy widens what a Transport repeats, for an API whose answers allow
// it. The zero Policy repeats only what the Transport's doc lists.
type Policy struct {
	// DropMethods are methods besides GET and HEAD repeated after a
	// transport error: a DELETE where a repeat answering 404 reads as done.
	DropMethods []string
	// ReadStatuses are statuses besides 502, 503 and 504 a GET or HEAD is
	// repeated on: a 500 of an API that answers it under load.
	ReadStatuses []int
	// BackoffOnly ignores Retry-After: the waits stay the doubling backoff
	// (an API whose Retry-After would hold a create for minutes).
	BackoffOnly bool
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
	resp, err := base.RoundTrip(withUserAgent(req))
	if err != nil {
		return nil, err
	}
	return HTMLAsBadGateway(resp), nil
}

// HTMLAsBadGateway turns a 2xx answer with an HTML page into a 502: the
// APIs of Cloud.ru answer JSON (S3 — XML), so an HTML page is a gateway's
// or a proxy's, not the service's. Read as a success it reported a delete,
// a create or a start that never happened (break r3b, R3B-1); every client
// reports a 502 as an API error. The compute adapter, which may be given
// any http.Client, applies it itself.
func HTMLAsBadGateway(resp *http.Response) *http.Response {
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.EqualFold(mt, "text/html") {
		return resp
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	body := `{"message":"Cloud.ru answered an HTML page with HTTP ` + strconv.Itoa(resp.StatusCode) + ` instead of the API's answer"}`
	h := resp.Header.Clone()
	h.Set("Content-Type", "application/json")
	h.Del("Content-Length")
	return &http.Response{
		Status:        "502 Bad Gateway",
		StatusCode:    http.StatusBadGateway,
		Proto:         resp.Proto,
		ProtoMajor:    resp.ProtoMajor,
		ProtoMinor:    resp.ProtoMinor,
		Header:        h,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       resp.Request,
	}
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
			// A certificate the system does not trust stays untrusted.
			resend := readMethod(req.Method) || slices.Contains(t.Policy.DropMethods, req.Method)
			if last || !resend || !replayable || clerrors.TLSUntrusted(err) {
				return nil, err
			}
		} else {
			retry := replayable && (resp.StatusCode == http.StatusTooManyRequests ||
				readMethod(req.Method) && (resp.StatusCode == http.StatusBadGateway ||
					resp.StatusCode == http.StatusServiceUnavailable ||
					resp.StatusCode == http.StatusGatewayTimeout ||
					slices.Contains(t.Policy.ReadStatuses, resp.StatusCode)))
			if !retry || last {
				return HTMLAsBadGateway(resp), nil
			}
			if d, ok := retryAfter(resp.Header.Get("Retry-After")); ok && !t.Policy.BackoffOnly {
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
