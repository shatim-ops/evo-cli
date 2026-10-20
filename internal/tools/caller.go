// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	stderrors "errors"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// Caller is a command of the CLI that calls an engine tool in its own
// process: evo deploy, destroy, logs, status, list-zones, list-images and
// project list (internal/cli, spec REQ-4). The tool answers it as it
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
// Only internal/cli makes a Caller: an MCP call has none, and a tool
// called without one gates on confirmed=true as always.
type Caller struct {
	// Printer receives the engine's output; nil keeps it in the answer.
	Printer *output.Printer
	// Gate asks for consent to what answer — the tool's answer without
	// confirmed=true — describes: a prompt, --yes or --dry-run. human is
	// the engine's own words for it (destroy, an update by deploy), nil
	// when the answer has them (the preview of a new VM). Without a Gate
	// the tool gates on confirmed=true.
	Gate func(ctx context.Context, answer string, human *confirm.Preview) (bool, error)
	// DryRun is the command's --dry-run: nothing is locked or changed.
	DryRun bool
	// NonInteractive is deploy --non-interactive: never prompt, Ctrl-C
	// included.
	NonInteractive bool
	// Follow is logs --follow: MCP has no stream, so the tool has no such
	// argument (CLI only).
	Follow bool
	// Err is set to the error of an error answer.
	Err error
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

// live is the Caller of a call the engine shows and gates itself, nil
// otherwise.
func live(ctx context.Context) *Caller {
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
