// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
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

// inRequest reports whether id is in a header of req other than its own
// X-Request-Id: an answer that echoes a key or a token of the request
// (a short opaque token has the form of an id) names no request, and its
// value is a secret that must not reach the text of an error (#1751).
// traced asks of the answer's request too: a renewed token is set on a
// clone of the caller's request (renewing). The case is not compared: an
// echo of the token in another case is the same secret (#1751, `Bearer
// AbCd-opaque` answered as `abcd-opaque`).
func inRequest(req *http.Request, id string) bool {
	id = strings.ToLower(id)
	for name, vs := range req.Header {
		if http.CanonicalHeaderKey(name) == RequestIDHeader {
			continue
		}
		for _, v := range vs {
			if strings.Contains(strings.ToLower(v), id) {
				return true
			}
		}
	}
	return false
}
