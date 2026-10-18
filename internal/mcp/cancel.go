// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
)

// Cancellation of tool calls. A call runs under a context that ends when
//
//   - stdio: stdin is closed (the client is gone: MCP's way to stop a stdio
//     server) or the process gets SIGINT/SIGTERM;
//   - HTTP: the client closes the connection of the request, deletes its
//     session (DELETE /mcp), or the server shuts down;
//   - either: the client sends notifications/cancelled with the call's
//     requestId (MCP 2025-06-18, "Cancellation").
//
// There is no timeout: a deploy takes as long as it takes. Handlers see
// the cancellation as ctx.Done(); context.Cause names the reason.

// Reasons a call is cancelled (context.Cause of its context).
var (
	errCancelledByClient = errors.New("cancelled by the client (notifications/cancelled)")
	errClientGone        = errors.New("the HTTP client closed the connection")
	errSessionDeleted    = errors.New("the MCP session was deleted")
	errServerStopping    = errors.New("the MCP server is shutting down")
)

// inflightCall is one cancellable tools/call.
type inflightCall struct {
	cancel context.CancelCauseFunc
	// byClient: cancelled by notifications/cancelled. The client expects no
	// answer then; the transports drop it where they can.
	byClient atomic.Bool
}

type callKey struct{ scope, id string }

// inflight tracks the running tool calls by (scope, request id). The scope
// is "" over stdio and the session id over HTTP: request ids are unique
// only within one session.
type inflight struct {
	mu    sync.Mutex
	calls map[callKey]*inflightCall
}

// requestKey canonicalises a JSON-RPC id (1 and 1 match, 1 and "1" do not).
func requestKey(id json.RawMessage) string {
	var b bytes.Buffer
	if json.Compact(&b, id) != nil {
		return string(id)
	}
	return b.String()
}

// begin registers a call with the given id and returns its context; done
// unregisters it and releases the context.
func (f *inflight) begin(parent context.Context, scope string, id json.RawMessage) (context.Context, *inflightCall, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	c := &inflightCall{cancel: cancel}
	k := callKey{scope, requestKey(id)}
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[callKey]*inflightCall{}
	}
	f.calls[k] = c // a reused id: the newest call is the one to cancel
	f.mu.Unlock()
	return ctx, c, func() {
		f.mu.Lock()
		if f.calls[k] == c {
			delete(f.calls, k)
		}
		f.mu.Unlock()
		cancel(nil)
	}
}

// cancelRequest handles notifications/cancelled. An unknown or finished
// request is ignored, as the spec allows.
func (f *inflight) cancelRequest(scope string, params json.RawMessage) bool {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return false
	}
	f.mu.Lock()
	c := f.calls[callKey{scope, requestKey(p.RequestID)}]
	f.mu.Unlock()
	if c == nil {
		return false
	}
	c.byClient.Store(true)
	c.cancel(errCancelledByClient)
	return true
}

// cancelScope cancels every call of a scope (a deleted HTTP session).
func (f *inflight) cancelScope(scope string, cause error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, c := range f.calls {
		if k.scope == scope {
			c.cancel(cause)
		}
	}
}

// mergeCancel returns a context that ends with a (and its cause) or with
// b (and cause).
func mergeCancel(a, b context.Context, cause error) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(a)
	stop := context.AfterFunc(b, func() { cancel(cause) })
	return ctx, func() {
		stop()
		if b.Err() != nil {
			cancel(cause) // b ended first; stop may have beaten the AfterFunc
		}
		cancel(nil)
	}
}
