// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	stderrors "errors"
	"io"
	"strings"
	"sync"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// Caller is a command of the CLI that calls an engine tool in its own
// process: evo deploy, destroy, logs, status, list-zones, list-images and
// project list (internal/cli, spec REQ-4), whoami and doctor (REQ-11). The tool answers it as it
// answers MCP — the same arguments, the same checks, the same answer; the
// Caller changes only how the work is shown and how consent is asked, as
// these commands showed and asked before they called the tool:
//
//   - Printer: the engine prints its steps, its progress and its result
//     there as it works (deploy, destroy, logs), instead of into the
//     answer's "log";
//   - Gate: consent is asked where the engine reaches its gate, after the
//     checks it makes before asking, with the tool's own
//     confirmation_required answer as the preview (deploy, destroy);
//   - Err: the error behind an error answer, as the engine returned it, so
//     that the command prints the message, the hint and the cause it
//     printed before.
//
// Every other command of the CLI — the command trees and 'evo tools call'
// — calls its tool with a Caller that has only Progress: the stages of a
// long call, shown as it works, while the answer stays the one MCP gets.
//
// Only internal/cli makes a Caller: an MCP call has none, and a tool
// called without one gates on confirmed=true as always. A tool that calls
// another one (callTool: the steps of a stack) calls it without one: the
// person ran the outer tool, not its steps.
type Caller struct {
	// Printer receives the engine's output; nil keeps it in the answer.
	Printer *output.Printer
	// Progress shows the stages of a long call to the person while it
	// works (evo vm create: the floating IP, the request, the wait for the
	// VM, its address), on stderr: unlike Printer, nothing leaves the
	// answer, which stays the one MCP gets. nil shows nothing.
	Progress *output.Printer
	// Gate asks for consent to what answer — the tool's answer without
	// confirmed=true — describes: a prompt, --yes or --dry-run. human is
	// the engine's own words for it (destroy, an update by deploy), nil
	// when the answer has them (the preview of a new VM). Without a Gate
	// the tool gates on confirmed=true.
	Gate func(ctx context.Context, answer string, human *confirm.Preview) (bool, error)
	// Yes is consent given in advance (--yes; destroy --force): the call
	// is the one MCP makes with confirmed=true (TestParity_YesEqualsConfirmed),
	// whether or not the engine finds anything to ask about.
	Yes bool
	// DryRun is the command's --dry-run: nothing is locked or changed.
	DryRun bool
	// NonInteractive is deploy --non-interactive: never prompt, Ctrl-C
	// included.
	NonInteractive bool
	// Follow is logs --follow: MCP has no stream, so the tool has no such
	// argument (CLI only).
	Follow bool
	// Verbose is --verbose: doctor's probes name the HTTP status they got
	// (the API root answers 404, which reads as an error without it).
	Verbose bool
	// Doctor is the CLI's part of doctor's answer (CLI only): the checks of
	// the machine the command runs on — the binary, the config files and
	// their modes, kubectl and helm in PATH — around the tool's own, which
	// MCP gets: Doctor(own) is every check, in the order the command shows
	// them. Over MCP they would be the server's paths and programs.
	Doctor func(own []DoctorCheck) []DoctorCheck
	// Err is set to the error of an error answer.
	Err error

	// answeredYes is set when Gate answered yes (ask).
	answeredYes bool
}

// ask is Gate, noting a yes: the tools call it, never Gate itself, so the
// journal knows the consent the engine's gate got (consented, FB140-1).
func (c *Caller) ask(ctx context.Context, answer string, human *confirm.Preview) (bool, error) {
	ok, err := c.Gate(ctx, answer, human)
	if ok && err == nil {
		c.answeredYes = true
	}
	return ok, err
}

// consented reports whether the person consented to the call: in advance
// (Yes) or at the gate. --dry-run only looks, so it never consents.
func (c *Caller) consented() bool {
	return !c.DryRun && (c.Yes || c.answeredYes)
}

type callerKey struct{}

// WithCaller is ctx for a call of a tool by the CLI command c.
func WithCaller(ctx context.Context, c *Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// callerOf is the Caller of the call, nil for MCP.
func callerOf(ctx context.Context) *Caller {
	c, _ := ctx.Value(callerKey{}).(*Caller)
	return c
}

// silent is the progress printer of a call nobody watches: it shows
// nothing.
var silent = output.NewWithWriter(io.Discard, output.ModeJSON, false)

type progressKey struct{}

// WithProgress is ctx for a call whose MCP client asked for its progress
// (a progressToken): every line of progress the CLI shows the person on
// stderr goes to send as well, one line at a time, as the CLI prints it
// without a terminal.
func WithProgress(ctx context.Context, send func(line string)) context.Context {
	return context.WithValue(ctx, progressKey{}, send)
}

// progressOf is the printer of the call's progress — Caller.Progress, or
// the lines of an MCP client that asked for them (WithProgress) — and
// whether someone watches it; a printer that shows nothing otherwise.
func progressOf(ctx context.Context) (*output.Printer, bool) {
	if c := callerOf(ctx); c != nil && c.Progress != nil {
		return c.Progress, true
	}
	if send, _ := ctx.Value(progressKey{}).(func(string)); send != nil {
		return output.NewWithWriter(&lineWriter{send: send}, output.ModePlain, false), true
	}
	return silent, false
}

// stepOf is ctx for a tool a tool calls (callTool: the steps of a stack):
// the step is not the call the person or the MCP client made, so it has no
// Caller and no progress of its own — its progress and its error are the
// outer tool's to show, alike on both surfaces.
func stepOf(ctx context.Context) context.Context {
	return context.WithValue(WithCaller(ctx, nil), progressKey{}, (func(string))(nil))
}

// lineWriter hands each line written to it to send, trimmed; blank lines
// are dropped. A line is sent once its end is written.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	send func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			return len(p), nil
		}
		if line := strings.TrimSpace(string(w.buf[:i])); line != "" {
			w.send(line)
		}
		w.buf = w.buf[i+1:]
	}
}

// liveCaller is the Caller of a call the engine shows and gates itself,
// nil otherwise.
func liveCaller(ctx context.Context) *Caller {
	if c := callerOf(ctx); c != nil && c.Printer != nil && c.Gate != nil {
		return c
	}
	return nil
}

// failed is errAnswer that also hands err to the CLI command calling the
// tool (Caller.Err).
func failed(ctx context.Context, err error, fields map[string]any) string {
	if c := callerOf(ctx); c != nil {
		c.Err = err
	}
	return errAnswer(err, fields)
}

// OrganizationAnswered reports whether err is an HTTP answer of the
// Organization API (org_projects): 'evo project list' showed any such
// answer as a note, not as a failure, until v2.0.
func OrganizationAnswered(err error) bool {
	var he *adminHTTPError
	return stderrors.As(err, &he)
}
