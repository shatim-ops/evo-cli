// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package httpretry is an http.RoundTripper that repeats requests Cloud.ru
// did not apply: a dropped connection on a read, 429, and 502/503/504 on a
// read. The compute adapter keeps its own loop (internal/provision); the
// other service clients get this transport.
package httpretry

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/product"
)

// HTTP/2 health check of the base transport: a connection that read
// nothing for h2ReadIdle is pinged, and one that does not answer the ping
// in h2PingTimeout is closed, so the next request dials again. Without it a
// long-lived server (evo mcp) kept a connection that died behind a proxy,
// and every call on it waited for its client's timeout (60 s) until the
// server was restarted (LLM run of Grok on 1.6.0, G2-6).
const (
	h2ReadIdle    = 30 * time.Second
	h2PingTimeout = 15 * time.Second
)

// MinTLS is the oldest TLS evo speaks: its own declaration, not the default
// of the Go release it is built with (release plan 1.9). TLS 1.3 as the
// minimum waits for a probe of every address evo calls.
const MinTLS = tls.VersionTLS12

// NewBaseTransport is a new transport with the settings of
// http.DefaultTransport (the proxy of the environment, dial and TLS
// timeouts, HTTP/2), the minimum of TLS (MinTLS) and the health check of an
// HTTP/2 connection. Each call makes its own, for a caller that changes it
// (verify's TLS).
func NewBaseTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	}
	tr.TLSClientConfig.MinVersion = MinTLS
	tr.HTTP2 = &http.HTTP2Config{SendPingTimeout: h2ReadIdle, PingTimeout: h2PingTimeout}
	return tr
}

// DefaultBase is the transport under every HTTP client of evo that sets no
// Base of its own: the retrying Transport, UserAgent and the clients built
// on them. It is read at each request, so a test that answers for the cloud
// replaces it alone.
var DefaultBase http.RoundTripper = NewBaseTransport()

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
	Base    http.RoundTripper // nil: DefaultBase
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
	// Keep404 leaves a 404 without the service's body as it is
	// (RouteNotFoundAsBadGateway): an API that answers 404 to a property
	// of the key rather than to a resource, in a form not probed.
	Keep404 bool
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
	return DefaultBase
}

func readMethod(m string) bool { return m == http.MethodGet || m == http.MethodHead }

type writesKey struct{}

// writes counts the writes of a call; mark is told before each write
// until it says the call is marked, and its error is the answer of every
// write of the call.
type writes struct {
	n      atomic.Int64
	mark   func() (marked bool, err error)
	mu     sync.Mutex
	marked bool
	err    error
}

// CountWrites is ctx for a call whose writes are counted: every request
// that is not a read, sent with this context through any client of evo
// (Transport or UserAgent), is one (Writes), and so is a write that goes
// out other than over HTTP, told by NoteWrite. mark, when not nil, is told
// before a write goes out, until it says the call is marked — a write
// before there is anything to mark does not spend it; an error of it keeps
// that write and every later one of the call from going out. A caller that
// must know whether anything was sent to change the cloud — a call cut
// before an answer changed nothing if it sent no write — counts by it.
func CountWrites(ctx context.Context, mark func() (marked bool, err error)) context.Context {
	return context.WithValue(ctx, writesKey{}, &writes{mark: mark})
}

// Writes is how many writes were sent with ctx since CountWrites; counted
// is false for a context that does not count.
func Writes(ctx context.Context) (n int, counted bool) {
	c, ok := ctx.Value(writesKey{}).(*writes)
	if !ok {
		return 0, false
	}
	return int(c.n.Load()), true
}

// WriteRefused is the error mark answered for the call of ctx: its writes
// were kept back (CountWrites), and the answer of the call is this error,
// not what its handler made of a request that never went out. nil when
// nothing was kept back.
func WriteRefused(ctx context.Context) error {
	if c, ok := ctx.Value(writesKey{}).(*writes); ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.err
	}
	return nil
}

// NoteWrite counts one write of ctx right before it goes out. A write
// that goes out other than over HTTP — kubectl, helm, a command over ssh
// — is told by its sender; evo's HTTP clients tell their own. An error:
// the write must not go out (CountWrites).
func NoteWrite(ctx context.Context) error {
	if _, no := ctx.Value(notAChangeKey{}).(bool); no {
		return nil
	}
	c, ok := ctx.Value(writesKey{}).(*writes)
	if !ok {
		return nil
	}
	c.mu.Lock()
	if c.err == nil && c.mark != nil && !c.marked {
		c.marked, c.err = c.mark()
	}
	err := c.err
	c.mu.Unlock()
	if err != nil {
		return err
	}
	c.n.Add(1)
	return nil
}

type notAChangeKey struct{}

// NotAChange is ctx for a request that changes nothing in the cloud
// though it is not a read: the POST for a token (IAM, the OIDC of a
// cluster). It is not counted as a write of the call.
func NotAChange(ctx context.Context) context.Context {
	return context.WithValue(ctx, notAChangeKey{}, true)
}

type onceKey struct{}

// Once is ctx for a request sent once: an error or a status the Transport
// would repeat is the answer at once. For a read beside the one a call is
// for, whose failure the call names instead of waiting it out («Риск»,
// conclusion 209, У1: the interfaces vm_get reads beside the card).
func Once(ctx context.Context) context.Context {
	return context.WithValue(ctx, onceKey{}, true)
}

// IsOnce reports whether a request under ctx is sent once (Once).
func IsOnce(ctx context.Context) bool {
	_, once := ctx.Value(onceKey{}).(bool)
	return once
}

type countedKey struct{}

// noteWrite counts req when it is a write of a counting context, once: a
// Transport over UserAgent (the Compute client) counts it at the Transport,
// and the request it hands down says so (counted). An error: req must not
// go out.
func noteWrite(req *http.Request) (counted *http.Request, err error) {
	if readMethod(req.Method) || req.Context().Value(countedKey{}) != nil {
		return req, nil
	}
	if err := NoteWrite(req.Context()); err != nil {
		return nil, err
	}
	return req.WithContext(context.WithValue(req.Context(), countedKey{}, true)), nil
}

// UserAgent wraps base (nil: DefaultBase) so that a request
// without a User-Agent carries evo's (product.UserAgent), a request without
// an X-Request-Id a new one, and counts its writes and keeps its failures
// as Transport does (CountWrites, TraceRequests). For the clients that keep their
// own retry loop: the compute adapter, S3 and IAM.
func UserAgent(base http.RoundTripper) http.RoundTripper { return uaTransport{base} }

type uaTransport struct{ base http.RoundTripper }

func (t uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = DefaultBase
	}
	req, err := noteWrite(withHeaders(req))
	if err != nil {
		return nil, err
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		traced(req, nil, err)
		return nil, err
	}
	resp = answer(req, recordedBy(req, resp))
	traced(req, resp, nil)
	return resp, nil
}

// tokenRejected is told the bearer token of a request a Cloud.ru API
// answered 401 to (OnTokenRejected).
var tokenRejected atomic.Pointer[func(token string)]

// OnTokenRejected sets f to be told the bearer token of every request a
// Cloud.ru API answers 401 to, whichever client sent it: the token
// provider drops that token, so the next call exchanges the key again
// instead of sending a token the services reject until its deadline
// (G140-3). nil stops it.
func OnTokenRejected(f func(token string)) {
	if f == nil {
		tokenRejected.Store(nil)
		return
	}
	tokenRejected.Store(&f)
}

// tokenRenewed is asked for the token that replaces a refused one
// (OnTokenRenewed).
var tokenRenewed atomic.Pointer[func(ctx context.Context, token string) (string, bool)]

// OnTokenRenewed sets f to be asked, for the bearer token a service
// refused, the token that replaces it: a new one from the provider that
// issued it, and the request goes once more with that. A service client takes its token once, when it is built; after
// the machine slept the services refuse it, and a wait would end with
// the 401 of one poll while the operation goes on (process backlog 1.6,
// priority 1). nil stops it.
func OnTokenRenewed(f func(ctx context.Context, token string) (string, bool)) {
	if f == nil {
		tokenRenewed.Store(nil)
		return
	}
	tokenRenewed.Store(&f)
}

// renewedToken is req with the bearer token its provider renewed, or req
// as it is.
func renewedToken(req *http.Request) *http.Request {
	tok, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	f := tokenRenewed.Load()
	if !ok || f == nil {
		return req
	}
	fresh, ok := (*f)(req.Context(), tok)
	if !ok || fresh == tok {
		return req
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+fresh)
	return r
}

// answer is the response both transports return: an HTML page as a 502
// (HTMLAsBadGateway), and a 401 to a bearer token reported to
// OnTokenRejected.
func answer(req *http.Request, resp *http.Response) *http.Response {
	if resp.StatusCode == http.StatusUnauthorized {
		if tok, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer "); ok {
			if f := tokenRejected.Load(); f != nil {
				(*f)(tok)
			}
		}
	}
	return HTMLAsBadGateway(resp)
}

// HTMLAsBadGateway turns a 2xx answer with an HTML page into a 502: the
// APIs of Cloud.ru answer JSON (S3 — XML), so an HTML page is a gateway's
// or a proxy's, not the service's. Read as a success it reported a delete,
// a create or a start that never happened (break r3b, R3B-1); every client
// reports a 502 as an API error. Transport applies it to every answer it
// returns.
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
	return badGateway(resp, `{"message":"Cloud.ru answered an HTML page with HTTP `+strconv.Itoa(resp.StatusCode)+` instead of the API's answer"}`)
}

// RouteNotFoundAsBadGateway turns a 404 the service did not write into a
// 502 (D63): a route or an address that is not there, not a resource that
// is gone. Read as «not found», a moved API answered a delete with
// already_deleted while the resource lived on (E1). The service's 404 is
// JSON: an array (compute) or an object with its code and message (VPC,
// NLB, DNS, mk8s, AR, Secret Manager; api-probe 33 and 34, 2 of 2). The
// gateway's is empty, HTML, plain text, {"error_msg": …} (VPC, NLB,
// Container Apps), or grpc-gateway's routing error — code 5 with the
// message "Not Found" and nothing else (DNS, mk8s, AR), which no service
// gives a resource. A HEAD has no body to tell them by and stays a 404.
func RouteNotFoundAsBadGateway(req *http.Request, resp *http.Response) *http.Response {
	if resp.StatusCode != http.StatusNotFound || req.Method == http.MethodHead {
		return resp
	}
	head, _ := io.ReadAll(io.LimitReader(resp.Body, maxServiceBody))
	// A gateway's answer is short: one that fills the read is the service's.
	if len(head) == maxServiceBody || serviceNotFound(head) {
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
		return resp
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	return badGateway(resp, `{"message":"Cloud.ru answered 404 without the service's answer: `+routeNotFound+`"}`)
}

// routeNotFound is how the 502 of RouteNotFoundAsBadGateway says what it is.
const routeNotFound = "the route or the address of the service is not there, and the resource was not checked"

// RouteNotFound reports whether err carries the answer of
// RouteNotFoundAsBadGateway: whatever the client wrapped it in, its message
// keeps the body.
func RouteNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), routeNotFound)
}

// Replaced is the header of an answer the transport wrote in place of the
// API's (HTMLAsBadGateway, RouteNotFoundAsBadGateway): a 502 that was not
// repeated, whatever its method.
const Replaced = "Evo-Replaced-Answer"

// maxServiceBody is how much of a 404 serviceNotFound reads.
const maxServiceBody = 64 << 10

// serviceNotFound reports whether body is a service's answer to a resource
// that is not there (RouteNotFoundAsBadGateway).
func serviceNotFound(body []byte) bool {
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		return json.Valid(body)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return false
	}
	_, code := obj["code"]
	_, message := obj["message"]
	if _, gw := obj["error_msg"]; gw && !code && !message {
		return false
	}
	var routing struct {
		Code    json.Number `json:"code"`
		Message string      `json:"message"`
	}
	_ = json.Unmarshal(body, &routing)
	return routing.Code != "5" || routing.Message != http.StatusText(http.StatusNotFound)
}

// badGateway is resp replaced by a 502 with body, a JSON message.
func badGateway(resp *http.Response, body string) *http.Response {
	h := resp.Header.Clone()
	h.Set("Content-Type", "application/json")
	h.Set(Replaced, "1")
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

// withHeaders returns req with evo's User-Agent and a new X-Request-Id
// (RequestIDHeader), or req itself when it already has both: those of the
// caller stay. A RoundTripper must not change the caller's request.
func withHeaders(req *http.Request) *http.Request {
	ua, id := req.Header.Get("User-Agent") == "", req.Header.Get(RequestIDHeader) == ""
	if !ua && !id {
		return req
	}
	r := req.Clone(req.Context())
	if ua {
		r.Header.Set("User-Agent", product.UserAgent())
	}
	if id {
		r.Header.Set(RequestIDHeader, NewRequestID())
	}
	return r
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// One id for every attempt of the request: support sees them as one.
	req = withHeaders(req)
	resp, err := t.renewing(req)
	// The answer RoundTrip gives, whichever way it came — the first one or
	// the one to the renewed token (FB160-1) — is read for a 404 the
	// service did not write (D63).
	if err == nil && !t.Policy.Keep404 {
		resp = RouteNotFoundAsBadGateway(req, resp)
	}
	traced(req, resp, err)
	return resp, err
}

// renewing is roundTrip with one more try of a refused token, renewed.
func (t *Transport) renewing(req *http.Request) (*http.Response, error) {
	resp, err := t.roundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	// A refused token (answer has reported it) is tried once with the
	// one its provider renews: a 401 applied nothing. Refused again, it is
	// the key's, and the 401 is the answer.
	again := renewedToken(req)
	if again == req || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return resp, nil
	}
	if req.Body != nil && req.Body != http.NoBody {
		body, err := req.GetBody()
		if err != nil {
			return resp, nil
		}
		again.Body = body
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	return t.roundTrip(again)
}

func (t *Transport) roundTrip(req *http.Request) (*http.Response, error) {
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
		r, err := noteWrite(r)
		if err != nil {
			return nil, err
		}
		r, below := recordBelow(r)
		resp, err := t.base().RoundTrip(r)
		if err == nil && !*below {
			// The API's answer, before the transport reads it: each
			// attempt is one (EnvRecordAnswers).
			resp = recorded(r, resp)
		}
		last := attempt == MaxRetries || IsOnce(req.Context()) || req.Context().Err() != nil
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
				return answer(req, resp), nil
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
