// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// RequestIDHeader is the header that names a request to Cloud.ru: evo
// sends a new UUID in it with every request (withHeaders), and the error
// of a call names it (FailedRequest), so support finds the request by it
// (release plan 1.9). Most services answer an id of their own in it.
const RequestIDHeader = "X-Request-Id"

// NewRequestID is a random UUID, version 4: the X-Request-Id of a request
// a caller repeats itself (the IAM exchange), so its attempts share one.
func NewRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Failed is a request of a call the API answered with an error, or did not
// answer (Status 0): the id evo sent and the id the answer carried, "" when
// it carried none, or one not in the form of an id.
type Failed struct {
	Status   int
	Sent     string
	Answered string
	// write: the request was not a read (readMethod).
	write bool
}

// answeredID is the form of an id an answer may carry: the value is the
// service's, and it goes into the text of an error a model reads.
var answeredID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

type traceKey struct{}

type trace struct {
	mu     sync.Mutex
	failed []Failed
}

// TraceRequests is ctx for a call whose failed requests are kept
// (FailedRequest). Over a context that already traces, it is ctx: the
// steps of a call are the call's.
func TraceRequests(ctx context.Context) context.Context {
	if _, ok := ctx.Value(traceKey{}).(*trace); ok {
		return ctx
	}
	return context.WithValue(ctx, traceKey{}, &trace{})
}

// FailedRequest is the request of the call of ctx (TraceRequests) its
// error is about: the last write answered with status, or, with status 0,
// not answered at all — else the last read so answered. A call whose
// write is refused reads after it to say why, and that read may be
// answered alike (POST 403, then GET 403: #1754): the error is the
// write's.
func FailedRequest(ctx context.Context, status int) (Failed, bool) {
	t, ok := ctx.Value(traceKey{}).(*trace)
	if !ok {
		return Failed{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var read *Failed
	for i := len(t.failed) - 1; i >= 0; i-- {
		f := &t.failed[i]
		switch {
		case f.Status != status:
		case f.write:
			return *f, true
		case read == nil:
			read = f
		}
	}
	if read != nil {
		return *read, true
	}
	return Failed{}, false
}

// maxFailed bounds what a trace keeps: a call that polls through a long
// failure does not grow it without end.
const maxFailed = 64

// traced notes the outcome of req, sent with its id: an error, or an
// answer of 400 and above.
func traced(req *http.Request, resp *http.Response, err error) {
	t, ok := req.Context().Value(traceKey{}).(*trace)
	if !ok {
		return
	}
	f := Failed{Sent: req.Header.Get(RequestIDHeader), write: !readMethod(req.Method)}
	switch {
	case err != nil:
	case resp.StatusCode >= 400:
		f.Status = resp.StatusCode
		if id := resp.Header.Get(RequestIDHeader); answeredID.MatchString(id) && !inRequest(req, id) && (resp.Request == nil || !inRequest(resp.Request, id)) {
			f.Answered = id
		}
	default:
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.failed) == maxFailed {
		t.failed = append(t.failed[:0], t.failed[1:]...)
	}
	t.failed = append(t.failed, f)
}

// inRequest reports whether id echoes what req sent other than its own
// X-Request-Id — a header, the query or the body: an answer that echoes a
// key or a token of the request (a short opaque token has the form of an
// id) names no request, and its value is a secret that must not reach the
// text of an error (#1751). The secret of a key exchange is in the body
// («Риск», conclusion 202). An echo is id within a value of the request,
// or a value within id — the secret with a prefix or a suffix around it
// («Риск», conclusion 249). Case does not tell them apart: a service that
// echoes the value in another case echoes the same secret. traced asks of
// the answer's request too: a renewed token is set on a clone of the
// caller's request (renewing).
func inRequest(req *http.Request, id string) bool {
	id = strings.ToLower(id)
	echoes := func(v string) bool {
		if strings.Contains(strings.ToLower(v), id) {
			return true
		}
		for _, part := range strings.FieldsFunc(v, valueSeparator) {
			if len(part) >= minEchoValue && strings.Contains(id, strings.ToLower(part)) {
				return true
			}
		}
		return false
	}
	for name, vs := range req.Header {
		if http.CanonicalHeaderKey(name) == RequestIDHeader {
			continue
		}
		for _, v := range vs {
			if echoes(v) {
				return true
			}
		}
	}
	if req.URL != nil {
		if q, err := url.QueryUnescape(req.URL.RawQuery); echoes(req.URL.RawQuery) || err == nil && echoes(q) {
			return true
		}
	}
	return inBody(req, echoes)
}

// minEchoValue is the shortest value of a request whose presence within an
// answered id makes it an echo: shorter ones (true, GET, a port) are in
// many an id that echoes nothing.
const minEchoValue = 8

// valueSeparator splits a header, a query or a form into its values: a
// scheme from its token (Bearer), a name from its value, one pair from
// the next.
func valueSeparator(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(`,;=&"'`, r)
}

// maxEchoBody bounds the body inRequest reads again: a key exchange or a
// form is small, an upload is not where a credential goes.
const maxEchoBody = 64 << 10

// inBody reports whether the body req sent may hold what has looks for:
// read again (GetBody), as sent, form-decoded, or in a string of it as
// JSON, decoded (an escape does not hide a value). A body that cannot be
// read again whole — no GetBody, over maxEchoBody, a read that fails —
// may hold it: an answered value not shown absent from the request is no
// id (#1933).
func inBody(req *http.Request, has func(string) bool) bool {
	if req.GetBody == nil {
		return req.Body != nil && req.Body != http.NoBody
	}
	if req.ContentLength > maxEchoBody {
		return true
	}
	rc, err := req.GetBody()
	if err != nil {
		return true
	}
	defer rc.Close()
	b, _, err := peek(rc, maxEchoBody+1)
	if err != nil || len(b) > maxEchoBody {
		return true
	}
	if has(string(b)) {
		return true
	}
	if q, err := url.QueryUnescape(string(b)); err == nil && has(q) {
		return true
	}
	return inJSON(b, has)
}

// inJSON reports whether a string or a number of b, read as JSON, holds
// what has looks for: keys and values alike. A body that is not JSON
// holds none.
func inJSON(b []byte, has func(string) bool) bool {
	if !json.Valid(b) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	for {
		tok, err := d.Token()
		if err != nil {
			return false
		}
		switch v := tok.(type) {
		case string:
			if has(v) {
				return true
			}
		case json.Number:
			if has(v.String()) {
				return true
			}
		}
	}
}
