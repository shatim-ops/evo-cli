// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package mcp implements a Model Context Protocol (MCP) server that exposes
// evo operations as tools for AI agents. It speaks JSON-RPC 2.0 over
// stdio (newline-delimited JSON, Run) or over Streamable HTTP (http.go).
//
// IMPORTANT: in stdio mode stdout is reserved for the JSON-RPC channel. Nothing else may be
// written to it or the protocol stream is corrupted. The engine of every
// tool call prints to stderr (internal/cli mcpEngineFactory); tools that
// return the progress in their answer capture it in a buffer
// (internal/tools); diagnostics go to stderr.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/product"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// supportedProtocolVersions are the MCP revisions the server speaks, newest
// first. None of them adds a field the server must send: tools/list and
// tools/call results are the same in all three.
var supportedProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// defaultProtocolVersion is the version answered when the client asks for none
// or for one the server does not support: the newest one (the client then
// decides whether it can talk to the server).
var defaultProtocolVersion = supportedProtocolVersions[0]

// supportedProtocol reports whether v is one of supportedProtocolVersions.
func supportedProtocol(v string) bool {
	for _, s := range supportedProtocolVersions {
		if s == v {
			return true
		}
	}
	return false
}

// serverName is the MCP server identity reported during initialize.
const serverName = "evo"

// EngineFactory builds an *engine.Engine from a set of config overrides; it
// is the registry's factory type, so one function serves both transports and
// the CLI.
type EngineFactory = tools.EngineFactory

// toolHandler executes a single tool call. It returns the textual result and a
// flag indicating whether the result represents a tool-level error (reported to
// the model via the MCP isError field, not as a JSON-RPC protocol error).
type toolHandler func(ctx context.Context, args map[string]any) (text string, isError bool)

// Tool is an MCP tool definition advertised via tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations Annotations    `json:"annotations"`
}

// Annotations are the MCP tool hints (spec 2025-03-26+): clients read them
// to approve reads without asking and to always ask before a destructive
// call. They are hints, not the gate: mutating tools still need
// confirmed=true. Clients of older revisions ignore the field.
//
// destructiveHint means something only for a mutation and defaults to true,
// so a mutation always carries it (false for one that only adds or changes
// in place) and a read leaves it out; readOnlyHint and idempotentHint
// default to false and are written only when true; openWorldHint defaults
// to true, which is what every evo tool is (cloud or state outside the
// conversation), so it is not written. tools/list goes to every session: no
// bytes for what the defaults already say.
type Annotations struct {
	ReadOnlyHint    bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  bool  `json:"idempotentHint,omitempty"`
}

// annotations derives the hints from the registry: a read is read-only and
// idempotent, a mutation is destructive by tools.IsDestructive. A tool that
// reveals a secret (tools.IsSensitive: secret_value, cert_private_key) is
// not read-only, so the client asks the person before the call; it changes
// nothing, so it is idempotent and says destructiveHint false (the default
// of a tool that is not read-only is true).
func annotations(t tools.Tool) Annotations {
	if tools.IsSensitive(t) {
		d := false
		return Annotations{DestructiveHint: &d, IdempotentHint: true}
	}
	a := Annotations{ReadOnlyHint: !t.Mutating, IdempotentHint: !t.Mutating}
	if t.Mutating {
		d := tools.IsDestructive(t)
		a.DestructiveHint = &d
	}
	return a
}

// Server is an MCP server exposing evo operations over stdio.
type Server struct {
	version  string
	tools    []Tool
	handlers map[string]toolHandler
	reg      *tools.Registry
	inflight inflight // running tool calls, for cancellation

	// enabled is the --toolsets view: tools/call accepts only these tools;
	// nil means every tool (the default). listed is what tools/list
	// advertises when it is not every tool (relist).
	listed  []Tool
	enabled map[string]bool

	// readOnly: SetReadOnly was called. gated holds, per tool, the handler
	// under its H11 gate: a restriction applied after SetReadOnly (those of
	// the transport, NewHTTPHandler) wraps it, not the gate, so the gate
	// stays the outermost layer (wrapHandler).
	readOnly bool
	gated    map[string]*tools.Handler

	// httpApplied records the restrictions of HTTPRestrictions applied to
	// the handlers (over HTTP and by --read-only), as "ID tool" (restrict),
	// for the closed-list test.
	httpApplied []string
}

// New creates an MCP server. factory builds engines per tool call (so each call
// reflects the current working directory, config, and SSH keys); version is the
// CLI version reported during initialize.
func New(factory EngineFactory, version string) *Server {
	s := &Server{
		version:  version,
		handlers: map[string]toolHandler{},
		reg:      tools.New(factory),
	}
	// The tools live in internal/tools, shared with the CLI; this package is
	// only the transport.
	for _, t := range s.reg.Tools() {
		s.tools = append(s.tools, Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
			Annotations: annotations(t)})
		s.handlers[t.Name] = toolHandler(t.Handler)
	}
	return s
}

// SetProjectBound binds the tools to the project of the server's
// configuration (evo mcp --allow-project, H9; tools.Registry.SetProjectBound),
// on either transport. Call it before the server serves.
func (s *Server) SetProjectBound() { s.reg.SetProjectBound() }

// ListTools returns the advertised tool definitions.
func (s *Server) ListTools() []Tool {
	if s.listed != nil {
		return s.listed
	}
	return s.tools
}

// relist sets what tools/list advertises: the tools of --toolsets, less
// those --read-only refuses every call of (tools.AlwaysNeedsConsent). nil
// listed is every tool.
func (s *Server) relist() {
	s.listed = nil
	if s.enabled == nil && !s.readOnly {
		return
	}
	s.listed = []Tool{}
	for i, t := range s.reg.Tools() {
		if (s.enabled != nil && !s.enabled[t.Name]) || (s.readOnly && tools.AlwaysNeedsConsent(t)) {
			continue
		}
		s.listed = append(s.listed, s.tools[i])
	}
}

// SetToolsets limits tools/list and tools/call to the tools of the named
// groups (tools.Groups(): deploy, vm, vpc, k8s, …); "all" or no group keeps
// every tool. The point is the agent's context: 154 tools are ~110 KB of
// tools/list in every session, deploy+vm+vpc are 26 (review r5 M3, as
// GitHub's MCP server --toolsets). It is not a permission boundary:
// stack_apply still drives the tools of other groups in-process, and the
// HTTP policy, --allow-project and --read-only hold either way.
func (s *Server) SetToolsets(groups []string) error {
	want, err := tools.ParseToolsets(groups)
	if err != nil {
		return err
	}
	s.enabled = nil
	if len(want) > 0 {
		in := map[string]bool{}
		for _, g := range want {
			in[g] = true
		}
		s.enabled = map[string]bool{}
		for _, t := range s.reg.Tools() {
			if in[t.Group] {
				s.enabled[t.Name] = true
			}
		}
	}
	s.relist()
	return nil
}

// --- JSON-RPC 2.0 wire types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`

	// Result and Error mark a response from the client. The server sends
	// no requests, so there is nothing to match it to: it is ignored.
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC standard error codes.
const (
	codeParseError     = -32700
	codeInvalidReq     = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// maxMessageSize bounds one message: a stdio line or an HTTP body.
const maxMessageSize = 16 << 20

// nullID is the id of an answer to a message whose id cannot be read.
var nullID = json.RawMessage("null")

// Run serves the JSON-RPC loop over the given reader/writer until EOF.
// Messages are newline-delimited JSON (MCP stdio transport): one message or
// a batch per line. A line longer than maxMessageSize is answered with an
// error and skipped; the loop goes on.
func (s *Server) Run(in io.Reader, out io.Writer) error {
	return s.RunContext(context.Background(), in, out)
}

// stdioQueue is how many lines are read ahead while a tool call runs: the
// reader must keep reading to see notifications/cancelled and EOF, and a
// client that floods requests meanwhile waits on the pipe again.
const stdioQueue = 64

// stdioShutdownGrace is how long RunContext waits, once ctx is done, for
// the running call to return after its cancellation; one that ignores its
// context is then left behind (the process exits).
var stdioShutdownGrace = 10 * time.Second

// stdioLine is one line of input, decoded by the reader goroutine.
type stdioLine struct {
	msgs    []incoming
	batch   bool
	tooLong bool
	calls   map[*rpcRequest]stdioCall // registered tools/call requests
	err     error                     // read error after this line (not EOF)
}

type stdioCall struct {
	ctx  context.Context
	call *inflightCall
	done func()
}

// RunContext is Run with cancellation: tool calls run under a context that
// ends when ctx does (SIGINT/SIGTERM), on a read error, or, for one call,
// on notifications/cancelled with its id. Closing stdin cancels nothing:
// lines read before EOF run to the end and are answered. When ctx is done RunContext returns
// ctx.Err() after the running call returns (at most stdioShutdownGrace).
func (s *Server) RunContext(ctx context.Context, in io.Reader, out io.Writer) error {
	serve, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	lines := make(chan stdioLine, stdioQueue)
	go s.readStdio(serve, stop, in, lines)

	enc := json.NewEncoder(out)
	for {
		var ln stdioLine
		var ok bool
		select {
		case ln, ok = <-lines:
		case <-ctx.Done():
			return ctx.Err()
		}
		if !ok {
			return nil // EOF
		}
		answered := make(chan any, 1)
		go func() { answered <- s.answerLine(serve, ln) }()
		var resp any
		select {
		case resp = <-answered:
		case <-ctx.Done():
			select {
			case resp = <-answered:
			case <-time.After(stdioShutdownGrace):
				return ctx.Err()
			}
		}
		if resp != nil {
			if err := enc.Encode(resp); err != nil && serve.Err() == nil {
				return fmt.Errorf("write response: %w", err)
			}
		}
		if ln.err != nil {
			return ln.err
		}
	}
}

// readStdio reads and decodes lines for RunContext. It acts on
// notifications/cancelled at once (the main loop may be busy with the very
// call to cancel) and registers each tools/call when it is read, so a call
// still waiting in the queue can be cancelled too.
func (s *Server) readStdio(serve context.Context, stop context.CancelCauseFunc, in io.Reader, lines chan<- stdioLine) {
	defer close(lines)
	r := bufio.NewReaderSize(in, 64*1024)
	for {
		line, tooLong, readErr := readLine(r, maxMessageSize)
		ln := stdioLine{tooLong: tooLong}
		if readErr != nil && readErr != io.EOF {
			ln.err = readErr
		}
		if !tooLong && len(bytes.TrimSpace(line)) > 0 {
			ln.msgs, ln.batch = decodeMessages(line)
			for i, m := range ln.msgs {
				if m.req == nil {
					continue
				}
				switch {
				case m.req.Method == "notifications/cancelled":
					s.inflight.cancelRequest("", m.req.Params)
					ln.msgs[i] = incoming{} // handled; nothing to answer
				case m.req.Method == "tools/call" && !m.req.isNotification():
					ctx, call, done := s.inflight.begin(serve, "", m.req.ID)
					if ln.calls == nil {
						ln.calls = map[*rpcRequest]stdioCall{}
					}
					ln.calls[m.req] = stdioCall{ctx, call, done}
				}
			}
		}
		if ln.tooLong || len(ln.msgs) > 0 || ln.err != nil {
			select {
			case lines <- ln:
			case <-serve.Done():
				// RunContext may have returned (signal): nobody reads.
				for _, c := range ln.calls {
					c.done()
				}
				if readErr != nil {
					return
				}
				continue
			}
		}
		if readErr != nil {
			// EOF does not cancel: "printf … | evo mcp" and one-shot
			// clients close stdin right after the request and still wait
			// for the answer. A deploy cut short half-way costs more than a
			// finished one nobody reads; the calls already read run to the
			// end and RunContext returns after answering them.
			if readErr != io.EOF {
				stop(readErr)
			}
			return
		}
	}
}

// answerLine answers one stdio line. A tool call cancelled by
// notifications/cancelled gets no answer (MCP: the receiver SHOULD NOT
// respond to a cancelled request).
func (s *Server) answerLine(serve context.Context, ln stdioLine) any {
	if ln.tooLong {
		return errorResponse(nullID, codeInvalidReq, fmt.Sprintf("message larger than %d bytes", maxMessageSize))
	}
	var out []*rpcResponse
	for _, m := range ln.msgs {
		switch {
		case m.bad != nil:
			out = append(out, m.bad)
		case m.req != nil:
			ctx := serve
			c, registered := ln.calls[m.req]
			if registered {
				ctx = c.ctx
			}
			resp := s.handle(ctx, "", m.req)
			if registered {
				c.done()
				if c.call.byClient.Load() {
					resp = nil
				}
			}
			if resp != nil {
				out = append(out, resp)
			}
		}
	}
	switch {
	case len(out) == 0:
		return nil
	case ln.batch:
		return out
	default:
		return out[0]
	}
}

// readLine reads one line. A line longer than max is read to its end and
// dropped (tooLong), so the next line is read normally.
func readLine(r *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	for {
		chunk, err := r.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(chunk) > max+1 { // +1: the newline
				tooLong, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		if err != bufio.ErrBufferFull {
			return line, tooLong, err
		}
	}
}

// incoming is one decoded message: a request (or notification) to dispatch,
// or the error answer to a message that is not one. Both are nil for a
// response from the client, which needs no answer.
type incoming struct {
	req *rpcRequest
	bad *rpcResponse
}

// decodeMessages splits a stdio line or an HTTP body into messages; batch
// reports a non-empty JSON array (answered with an array).
func decodeMessages(data []byte) (msgs []incoming, batch bool) {
	data = bytes.TrimSpace(data)
	// JSON-RPC text is UTF-8. encoding/json would put U+FFFD in place of
	// a bad byte and the call would go to the cloud with another string
	// (break r3b, R3B-4): such a line is not a message.
	if !utf8.Valid(data) || !json.Valid(data) {
		return []incoming{{bad: errorResponse(nullID, codeParseError, "parse error")}}, false
	}
	if data[0] != '[' {
		return []incoming{decodeMessage(data)}, false
	}
	var raw []json.RawMessage
	_ = json.Unmarshal(data, &raw) // valid JSON starting with '['
	if len(raw) == 0 {
		return []incoming{{bad: errorResponse(nullID, codeInvalidReq, "invalid request: empty batch")}}, false
	}
	for _, m := range raw {
		msgs = append(msgs, decodeMessage(m))
	}
	return msgs, true
}

// decodeMessage decodes one valid JSON value as a JSON-RPC message.
func decodeMessage(raw json.RawMessage) incoming {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return incoming{bad: errorResponse(nullID, codeInvalidReq, "invalid request: not a JSON-RPC message object")}
	}
	if req.Method == "" {
		if len(req.Result) > 0 || len(req.Error) > 0 {
			return incoming{} // the client's response; the server sends no requests
		}
		id := req.ID
		if len(id) == 0 {
			id = nullID
		}
		return incoming{bad: errorResponse(id, codeInvalidReq, "invalid request: method is required")}
	}
	return incoming{req: &req}
}

// isNotification reports whether the request lacks an id (JSON-RPC notification,
// which must not receive a response).
func (r *rpcRequest) isNotification() bool {
	return len(r.ID) == 0
}

// handle dispatches a single request. Notifications are executed, but it
// returns nil for them: JSON-RPC forbids answering a notification. ctx is
// the context of a tool call; scope is the cancellation scope of
// notifications/cancelled ("" over stdio, the session over HTTP).
func (s *Server) handle(ctx context.Context, scope string, req *rpcRequest) *rpcResponse {
	resp := s.call(ctx, scope, req)
	if req.isNotification() {
		return nil
	}
	return resp
}

func (s *Server) call(ctx context.Context, scope string, req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "initialize":
		return s.ok(req, s.initializeResult(req.Params))

	case "notifications/initialized", "initialized":
		// Client acknowledgement; no response.
		return nil

	case "ping":
		return s.ok(req, map[string]any{})

	case "tools/list":
		return s.ok(req, map[string]any{"tools": s.ListTools()})

	case "tools/call":
		return s.handleToolCall(ctx, req)

	case "notifications/cancelled":
		s.inflight.cancelRequest(scope, req.Params)
		return nil

	default:
		return s.fail(req, codeMethodNotFound, "method not found: "+req.Method)
	}
}

// initializeResult builds the initialize response: the client's protocol
// version when the server supports it, otherwise the newest supported one.
func (s *Server) initializeResult(params json.RawMessage) map[string]any {
	protocolVersion := defaultProtocolVersion
	if len(params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(params, &p); err == nil && supportedProtocol(p.ProtocolVersion) {
			protocolVersion = p.ProtocolVersion
		}
	}

	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"title":   product.Name,
			"version": s.version,
		},
	}
}

// handleToolCall dispatches a tools/call request to the matching handler.
func (s *Server) handleToolCall(ctx context.Context, req *rpcRequest) *rpcResponse {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.fail(req, codeInvalidParams, "invalid tool call params: "+err.Error())
		}
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}

	// An unknown tool is a protocol error, -32602 (MCP 2025-06-18, Tools,
	// Error Handling); so is a tool outside --toolsets: tools/list does not
	// name it. Errors of a tool that ran stay results with isError
	// (release audit, R-07).
	if s.enabled != nil && !s.enabled[p.Name] {
		if _, exists := s.handlers[p.Name]; exists {
			return s.fail(req, codeInvalidParams, "Unknown tool: "+p.Name+" (not in this server's --toolsets)")
		}
	}
	handler, ok := s.handlers[p.Name]
	if !ok {
		return s.fail(req, codeInvalidParams, "Unknown tool: "+p.Name)
	}

	text, isErr := callHandler(ctx, handler, p.Name, p.Arguments)
	return s.toolResult(req, withApprovalNote(text), isErr)
}

// approvalNote goes into every confirmation_required answer. The gate stops
// a call, not an agent: a model that reads "call again with confirmed=true"
// can do just that on its own. Each tool words its own message; this one
// line, the same everywhere, says whose decision it is.
const approvalNote = "Stop here. Show this preview to the user and ask for explicit approval. " +
	"Call again with confirmed=true only after the user approved this action in the conversation; never decide it yourself."

// withApprovalNote adds approvalNote to a confirmation_required answer and
// leaves every other answer as it is. Re-encoding sorts the keys and
// indents: tools already answer that way (toJSON over a map), so nothing a
// client relies on changes.
func withApprovalNote(text string) string {
	if !bytes.Contains([]byte(text), []byte(`"confirmation_required"`)) {
		return text
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.UseNumber() // IDs and sizes come back exactly as the tool wrote them
	var obj map[string]any
	if dec.Decode(&obj) != nil || obj["status"] != "confirmation_required" {
		return text
	}
	if _, ok := obj["approval"]; ok {
		return text
	}
	obj["approval"] = approvalNote
	b, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return text
	}
	return string(b)
}

// panicLog receives the stack of a panicking tool handler (stderr: in stdio
// mode stdout is the protocol channel).
var panicLog io.Writer = os.Stderr

// callHandler runs a tool handler. A panic becomes a tool error: one broken
// handler must not take the server down, and with it every other session.
func callHandler(ctx context.Context, h toolHandler, name string, args map[string]any) (text string, isErr bool) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(panicLog, "evo mcp: tool %s panicked: %v\n%s", name, r, debug.Stack())
			text, isErr = tools.ErrResult(fmt.Errorf("internal error: tool %s panicked: %v", name, r)), true
		}
	}()
	return h(ctx, args)
}

// toolResult wraps a tool's textual output in the MCP tools/call result shape.
func (s *Server) toolResult(req *rpcRequest, text string, isError bool) *rpcResponse {
	return s.ok(req, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	})
}

// ok builds a success response carrying result.
func (s *Server) ok(req *rpcRequest, result any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// fail builds a JSON-RPC protocol error response.
func (s *Server) fail(req *rpcRequest, code int, msg string) *rpcResponse {
	return errorResponse(req.ID, code, msg)
}

func errorResponse(id json.RawMessage, code int, msg string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}
