// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	crand "crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Streamable HTTP transport (MCP 2025-03-26 / 2025-06-18):
//
//	POST   /mcp      one JSON-RPC message or a batch; the answer is JSON, or an
//	                 SSE stream for tools/call when the client accepts it
//	GET    /mcp      SSE stream for server-initiated messages (keep-alives only:
//	                 the server sends no requests of its own)
//	DELETE /mcp      ends the session named by Mcp-Session-Id
//	GET    /healthz  liveness probe, no authentication
//
// The dispatch (handle) is shared with the stdio transport, so a request
// gets the same answer over both.

// HTTPOptions configures the HTTP transport.
type HTTPOptions struct {
	// BearerToken, when set, is required as "Authorization: Bearer <token>"
	// on every /mcp request. Empty means no authentication: the caller
	// allows that only on a loopback address.
	BearerToken string

	// AllowSecretExport lets k8s_kubeconfig embed a cluster token and
	// cert_private_key return a private key. Off by default: over HTTP the
	// answer leaves the machine the credentials live on.
	AllowSecretExport bool

	// Logger receives one line per tool call (name, status, duration; never
	// arguments or results) and transport events. Nil disables logging.
	Logger *slog.Logger

	// KeepAlive is the interval of SSE comments that keep proxies from
	// closing a quiet stream. Zero means 15 s.
	KeepAlive time.Duration
}

// Limits of the HTTP transport.
const (
	httpMaxBody      = 4 << 20 // below the stdio line: 32 calls hold at most 128 MiB of bodies
	httpMaxSessions  = 10000
	httpMaxBatch     = 100 // messages in one POST: a batch of tools/list would otherwise answer ~40 KB per 30-byte request
	httpMaxToolCalls = 32  // tool calls running at once; more are refused, not queued
	httpSessionTTL   = 24 * time.Hour
	defaultKeepAlive = 15 * time.Second
	headerSessionID  = "Mcp-Session-Id"
	headerProtocol   = "Mcp-Protocol-Version"
)

// httpBodyTimeout is how long a client may take to send a request body
// (4 MiB at ~0.5 Mbit/s). A variable only so that tests can shorten it.
var httpBodyTimeout = 60 * time.Second

// HTTPHandler serves the MCP Streamable HTTP transport.
type HTTPHandler struct {
	srv  *Server
	opts HTTPOptions

	mu       sync.Mutex
	sessions map[string]time.Time // id -> last use

	calls chan struct{} // semaphore: running tool calls

	done     chan struct{}
	doneOnce sync.Once

	// base ends when the handler is closed (the server stops): every tool
	// call runs under it.
	base     context.Context
	stopBase context.CancelCauseFunc
}

// NewHTTPHandler returns the HTTP transport of s. It applies the HTTP-mode
// tool restrictions (see restrictForHTTP) to s, so s must not be served over
// stdio afterwards.
func NewHTTPHandler(s *Server, opts HTTPOptions) *HTTPHandler {
	if opts.KeepAlive <= 0 {
		opts.KeepAlive = defaultKeepAlive
	}
	s.restrictForHTTP(opts.AllowSecretExport)
	base, stopBase := context.WithCancelCause(context.Background())
	return &HTTPHandler{
		srv:      s,
		opts:     opts,
		sessions: map[string]time.Time{},
		calls:    make(chan struct{}, httpMaxToolCalls),
		done:     make(chan struct{}),
		base:     base,
		stopBase: stopBase,
	}
}

// Close ends open SSE streams; http.Server.Shutdown would otherwise wait for
// them forever. Running tool calls are cancelled.
func (h *HTTPHandler) Close() {
	h.doneOnce.Do(func() {
		h.stopBase(errServerStopping)
		close(h.done)
	})
}

// ServeHTTP implements http.Handler.
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`+"\n")
	case "/mcp":
		h.serveMCP(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *HTTPHandler) serveMCP(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		h.logf(slog.LevelWarn, "unauthorized request", "method", r.Method, "remote", r.RemoteAddr)
		w.Header().Set("WWW-Authenticate", `Bearer realm="evo"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// DNS rebinding: a browser page must not drive a local server. Without
	// a token the server listens on loopback only, and a page whose name
	// resolves to 127.0.0.1 sends its own name as both Host and Origin,
	// which passes the same-origin check: the Host must be loopback too.
	if h.opts.BearerToken == "" && !LoopbackHost(r.Host) {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	if !originAllowed(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if v := r.Header.Get(headerProtocol); v != "" && !supportedProtocol(v) {
		http.Error(w, "unsupported "+headerProtocol+": "+v, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPost:
		h.post(w, r)
	case http.MethodGet:
		h.stream(w, r)
	case http.MethodDelete:
		id := r.Header.Get(headerSessionID)
		if id == "" {
			http.Error(w, "missing "+headerSessionID, http.StatusBadRequest)
			return
		}
		if !h.dropSession(id) {
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		// The client is done with the session: nobody awaits its calls.
		h.srv.inflight.cancelScope(id, errSessionDeleted)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// authorized checks the bearer token in constant time. The scheme is
// case-insensitive: "bearer <token>" is as good as "Bearer <token>"
// (RFC 9110 §11.1; release audit, R-08).
func (h *HTTPHandler) authorized(r *http.Request) bool {
	if h.opts.BearerToken == "" {
		return true
	}
	scheme, got, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(h.opts.BearerToken)) == 1
}

// originAllowed accepts requests without Origin (non-browser clients), from
// a loopback origin, or from the origin of the server itself.
func originAllowed(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	if LoopbackHost(u.Host) {
		return true
	}
	return strings.EqualFold(u.Host, r.Host)
}

// LoopbackHost reports whether a host[:port] names the loopback interface
// (localhost, 127.0.0.0/8, ::1). The mcp command uses it to allow --auth none
// only there.
func LoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// post handles one JSON-RPC message or a batch of them.
func (h *HTTPHandler) post(w http.ResponseWriter, r *http.Request) {
	// A page in a browser may POST text/plain to any address without a
	// CORS preflight, and its Origin may be another loopback port: with
	// --auth none that reached destroy confirmed=true (security review
	// S1-app 1). application/json makes the browser ask first, and the
	// server answers no CORS. MCP clients send it as the spec requires.
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	// ReadHeaderTimeout covers the headers only; without a deadline a
	// client could trickle the body forever, holding the connection. The
	// deadline is lifted once the body is in: a tool call may run for many
	// minutes. On a read error it stays, or net/http would block draining
	// the rest of the body before it sends the error.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(httpBodyTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, httpMaxBody))
	if err == nil {
		_ = rc.SetReadDeadline(time.Time{})
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			http.Error(w, "request body not received in time", http.StatusRequestTimeout)
			return
		}
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	msgs, batch := decodeMessages(body)
	if len(msgs) > httpMaxBatch {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse(nullID, codeInvalidReq,
			fmt.Sprintf("batch of %d messages: at most %d per request", len(msgs), httpMaxBatch)))
		return
	}
	if !batch && msgs[0].bad != nil {
		writeJSON(w, http.StatusBadRequest, msgs[0].bad)
		return
	}
	var reqs []*rpcRequest
	for _, m := range msgs {
		if m.req != nil {
			reqs = append(reqs, m.req)
		}
	}

	initialize := false
	for _, req := range reqs {
		if req.Method == "initialize" {
			initialize = true
		}
	}
	if initialize && len(msgs) > 1 {
		writeJSON(w, http.StatusBadRequest, errorResponse(nullID, codeInvalidReq, "initialize must not be part of a batch"))
		return
	}
	// A client without a session id is served statelessly: the server
	// keeps no per-session state, the id only lets a client notice a
	// restart (404 → initialize again) and scopes notifications/cancelled
	// and DELETE to the session's own calls.
	scope := r.Header.Get(headerSessionID)
	if scope != "" && !initialize && !h.touchSession(scope) {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	if initialize {
		scope = ""
	}
	// A tool call ends with the server (Close) or with this request: the
	// client closed the connection, so nobody waits for the answer, and
	// without resumable streams it could not get it later anyway.
	ctx, release := mergeCancel(h.base, r.Context(), errClientGone)
	defer release()

	// Responses and notifications only: nothing to answer.
	hasRequest := false
	for _, m := range msgs {
		if m.bad != nil || (m.req != nil && !m.req.isNotification()) {
			hasRequest = true
		}
	}
	if !hasRequest {
		for _, req := range reqs {
			h.dispatch(ctx, scope, req)
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if initialize {
		w.Header().Set(headerSessionID, h.newSession())
	}

	if !batch && reqs[0].Method == "tools/call" && acceptsSSE(r) {
		h.postSSE(ctx, w, r, scope, reqs[0])
		return
	}

	// A plain JSON answer is sent even for a call the client cancelled:
	// the POST needs a body, and the client ignores it (MCP).
	var out []*rpcResponse
	for _, m := range msgs {
		if m.bad != nil {
			out = append(out, m.bad)
		} else if m.req != nil {
			if resp, _ := h.dispatch(ctx, scope, m.req); resp != nil {
				out = append(out, resp)
			}
		}
	}
	switch {
	case len(out) == 0:
		w.WriteHeader(http.StatusAccepted)
	case batch:
		writeJSON(w, http.StatusOK, out)
	default:
		writeJSON(w, http.StatusOK, out[0])
	}
}

// postSSE answers one tools/call as an SSE stream: comments keep the
// connection alive while a long call (deploy, cluster creation) runs, then
// the response arrives as a single "message" event.
func (h *HTTPHandler) postSSE(ctx context.Context, w http.ResponseWriter, r *http.Request, scope string, req *rpcRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		resp, _ := h.dispatch(ctx, scope, req)
		writeJSON(w, http.StatusOK, resp)
		return
	}
	type answer struct {
		resp      *rpcResponse
		cancelled bool
	}
	result := make(chan answer, 1)
	// When the client goes away, ctx ends and the call is cancelled: the
	// stream has no resumption (Last-Event-ID), the answer would be lost.
	// Mutating tools leave the state naming what they created.
	go func() {
		resp, cancelled := h.dispatch(ctx, scope, req)
		result <- answer{resp, cancelled}
	}()

	sseHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	tick := time.NewTicker(h.opts.KeepAlive)
	defer tick.Stop()
	for {
		select {
		case a := <-result:
			if a.cancelled || a.resp == nil {
				return // cancelled by the client: no answer (MCP)
			}
			data, _ := json.Marshal(a.resp)
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
			return
		case <-tick.C:
			_, _ = io.WriteString(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// stream serves GET /mcp: an SSE stream the server could push requests and
// notifications on. This server has none to send, so the stream only
// carries keep-alives until the client or the server goes away.
func (h *HTTPHandler) stream(w http.ResponseWriter, r *http.Request) {
	if !acceptsSSE(r) {
		http.Error(w, "GET /mcp requires Accept: text/event-stream", http.StatusNotAcceptable)
		return
	}
	if id := r.Header.Get(headerSessionID); id != "" && !h.touchSession(id) {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	sseHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	tick := time.NewTicker(h.opts.KeepAlive)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			_, _ = io.WriteString(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-h.done:
			return
		}
	}
}

// dispatch runs one message through the shared handler and logs tool
// calls. A tools/call with an id runs under its own context, cancellable by
// notifications/cancelled in scope; cancelled reports that it was.
func (h *HTTPHandler) dispatch(ctx context.Context, scope string, req *rpcRequest) (resp *rpcResponse, cancelled bool) {
	if req.Method != "tools/call" {
		return h.srv.handle(ctx, scope, req), false
	}
	select {
	case h.calls <- struct{}{}:
		defer func() { <-h.calls }()
	default:
		h.logf(slog.LevelWarn, "tool call refused: too many running", "limit", httpMaxToolCalls)
		if req.isNotification() {
			return nil, false
		}
		// The limit is this server's, not Cloud.ru's: SERVER_BUSY, not
		// API_RATE_LIMITED (release audit, L-03).
		return h.srv.toolResult(req, tools.ErrResult(clierrors.New(clierrors.CodeServerBusy, fmt.Sprintf(
			"server busy: %d tool calls are already running on this evo MCP server", httpMaxToolCalls),
			"Retry when one of the running calls has finished; Cloud.ru was not asked")), true), false
	}
	if !req.isNotification() {
		var call *inflightCall
		var done func()
		ctx, call, done = h.srv.inflight.begin(ctx, scope, req.ID)
		defer func() {
			cancelled = call.byClient.Load()
			done()
		}()
	}
	start := time.Now()
	resp = h.srv.handle(ctx, scope, req)
	if h.opts.Logger != nil {
		var p struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(req.Params, &p)
		status := callStatus(resp)
		if ctx.Err() != nil {
			status = "cancelled: " + context.Cause(ctx).Error()
		}
		h.opts.Logger.Info("tool call",
			"tool", p.Name,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
	return resp, false
}

// callStatus is "ok", "error" (tool-level, isError) or "rpc_error".
func callStatus(resp *rpcResponse) string {
	if resp == nil {
		return "none"
	}
	if resp.Error != nil {
		return "rpc_error"
	}
	if m, ok := resp.Result.(map[string]any); ok && m["isError"] == true {
		return "error"
	}
	return "ok"
}

func (h *HTTPHandler) logf(level slog.Level, msg string, args ...any) {
	if h.opts.Logger != nil {
		h.opts.Logger.Log(context.Background(), level, msg, args...)
	}
}

// --- sessions ---

func (h *HTTPHandler) newSession() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	id := hex.EncodeToString(b)

	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if len(h.sessions) >= httpMaxSessions {
		for k, seen := range h.sessions {
			if now.Sub(seen) > httpSessionTTL {
				delete(h.sessions, k)
			}
		}
		// Still full: forget the stalest one; its client re-initializes.
		if len(h.sessions) >= httpMaxSessions {
			var oldest string
			var oldestSeen time.Time
			for k, seen := range h.sessions {
				if oldest == "" || seen.Before(oldestSeen) {
					oldest, oldestSeen = k, seen
				}
			}
			delete(h.sessions, oldest)
		}
	}
	h.sessions[id] = now
	return id
}

func (h *HTTPHandler) touchSession(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen, ok := h.sessions[id]
	if !ok || time.Since(seen) > httpSessionTTL {
		delete(h.sessions, id)
		return false
	}
	h.sessions[id] = time.Now()
	return true
}

func (h *HTTPHandler) dropSession(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.sessions[id]
	delete(h.sessions, id)
	return ok
}

// --- helpers ---

func acceptsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// nginx and similar proxies buffer responses unless told otherwise.
	w.Header().Set("X-Accel-Buffering", "no")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}

// ListenAndServe listens on addr and serves the MCP HTTP transport until ctx is
// done, then shuts down gracefully. ready, if not nil, receives the bound
// address once the listener is open (useful with port 0).
func (s *Server) ListenAndServe(ctx context.Context, addr string, opts HTTPOptions, ready func(net.Addr)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	h := NewHTTPHandler(s, opts)
	hs := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: a tool call (deploy, cluster creation) may run
		// for many minutes; SSE keep-alives hold the connection.
	}
	if ready != nil {
		ready(ln.Addr())
	}
	h.logf(slog.LevelInfo, "MCP HTTP server listening",
		"addr", ln.Addr().String(), "auth", authMode(opts), "allow_secret_export", opts.AllowSecretExport)

	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()

	select {
	case err := <-errc:
		h.Close()
		return err
	case <-ctx.Done():
	}
	h.Close()
	h.logf(slog.LevelInfo, "MCP HTTP server shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil {
		return err
	}
	return nil
}

func authMode(o HTTPOptions) string {
	if o.BearerToken != "" {
		return "bearer"
	}
	return "none"
}
