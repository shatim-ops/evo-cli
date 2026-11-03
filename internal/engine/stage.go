// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/vmstage"
)

// InProgress ends a deploy whose bound (DeployOptions.Until, the budget of
// an MCP call, D69) came while the cloud or the VM still worked: not a
// failure. Phase is what goes on — "vm" (the VM is being created), "ssh"
// (it does not answer SSH yet), a stage on the VM ("docker",
// "compose-up"), which goes on without the call, or another step the bound
// cut ("upload", "verify"); Detail is its state in evo's words, Output the
// last line of a stage's log — the workload's words. The same deploy again
// goes on from there.
type InProgress struct {
	Phase  string
	Detail string
	Output string
}

func (p *InProgress) Error() string {
	return fmt.Sprintf("deploy is still in progress: %s (%s)", p.Phase, p.Detail)
}

// stages is the Runner of the long stages of deploy on the VM.
func (e *Engine) stages() vmstage.Runner {
	if e.Stages != nil {
		return e.Stages
	}
	if r, ok := e.Deployer.(vmstage.Runner); ok {
		return r
	}
	return vmstage.Over(e.Deployer)
}

// bounded is ctx with the bound of the deploy as its deadline (D69): every
// step under it — an SSH connect, an upload, a command, the floating IP —
// ends with the bound, not only the waits that check it («1.7 — релиз» on
// #769). Without a bound (the CLI) it is ctx.
func (e *Engine) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.until.IsZero() {
		return ctx, func() {}
	}
	return context.WithDeadline(ctx, e.until)
}

// atBound is the one point where the end of a deploy at its bound becomes
// InProgress of the step it was in (e.phase): an error that came once the
// bound had come is the step cut by its deadline, and the same deploy again
// goes on from there. A cancel of ctx — Ctrl-C, the MCP client — stays an
// error.
func (e *Engine) atBound(ctx context.Context, err error) error {
	var going *InProgress
	if err == nil || stderrors.As(err, &going) || ctx.Err() != nil || e.phase == "" || !e.past() {
		return err
	}
	return &InProgress{Phase: e.phase, Detail: errLine(err)}
}

// past reports whether the bound of the deploy has come.
func (e *Engine) past() bool {
	return !e.until.IsZero() && !time.Now().Before(e.until)
}

// stageInput is the input of a stage: its script and what it reads, so a
// stage done for another input runs again.
func stageInput(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:12])
}

// errStageWait: the stage still ran at the limit of runStage's wait.
var errStageWait = stderrors.New("the stage still runs")

// runStage runs the stage name on the VM for input — unless it runs, or is
// done for this input and rerun is false — and waits for its end until the bound of the
// deploy: *InProgress then, and the stage goes on. A limit (not zero) ends
// the wait too, with errStageWait: the CLI has no bound, and a stage that
// hangs is not waited for ever. A stage no process
// holds any more (lost: a reboot, a kill) is started again, and the call
// says so as an error: it does not pass for one going on. Each new last
// line of the stage's log is shown as a step.
func (e *Engine) runStage(ctx context.Context, name, input, script string, limit time.Time, rerun bool) (vmstage.Stage, error) {
	e.phase = name
	r := e.stages()
	st, err := r.Read(ctx, name)
	if err != nil {
		return st, err
	}
	if st.State == "lost" {
		return st, e.restartLost(ctx, r, name, input, script, st)
	}
	// A start detaches the stage at once; bounded by limit too, so one
	// that cannot (a VM where it runs in the session) ends with it.
	start := func() error {
		sctx := ctx
		if !limit.IsZero() {
			var cancel context.CancelFunc
			sctx, cancel = context.WithDeadline(ctx, limit)
			defer cancel()
		}
		_, err := r.Start(sctx, name, input, script)
		return err
	}
	if st.State != "running" && (rerun || st.State != "done" || st.Input != input) {
		if err := start(); err != nil {
			return vmstage.Stage{}, err
		}
	}
	t := e.timings()
	shown := ""
	for {
		if st, err = r.Read(ctx, name); err != nil {
			return st, err
		}
		if l := lastLines(st.LogTail, 1); l != "" && l != shown {
			e.Printer.Step(0, 0, clipLine(l))
			shown = l
		}
		switch st.State {
		case "done", "failed":
			if st.Input == input {
				return st, nil
			}
			// The end of a stage for another input (the compose file
			// changed while it ran): this input's own now.
			if err := start(); err != nil {
				return vmstage.Stage{}, err
			}
			continue
		case "lost":
			return st, e.restartLost(ctx, r, name, input, script, st)
		}
		if e.past() {
			return st, &InProgress{Phase: name, Detail: st.State, Output: shown}
		}
		if !limit.IsZero() && !time.Now().Before(limit) {
			return st, errStageWait
		}
		wait := t.StagePollInterval
		for _, end := range []time.Time{e.until, limit} {
			if !end.IsZero() {
				wait = min(wait, time.Until(end))
			}
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// restartLost starts again the stage the VM lost and answers what happened:
// the stage was cut on the VM, not by evo, and goes again.
func (e *Engine) restartLost(ctx context.Context, r vmstage.Runner, name, input, script string, st vmstage.Stage) error {
	if _, err := r.Start(ctx, name, input, script); err != nil {
		return err
	}
	ce := clerrors.New(clerrors.CodeSSHTimeout,
		i18n.Tf("the %s stage was interrupted on the VM (no process ran it any more: the VM rebooted, the process was stopped, or its start was cut off); evo started it again",
			"этап %s прервался на ВМ (его больше не выполнял ни один процесс: ВМ перезагрузилась, процесс остановили или его запуск оборвался); evo запустил его снова", name),
		i18n.T("Run deploy again to follow it; the last lines of the interrupted stage are in output.",
			"Повторите deploy, чтобы следить за ним; последние строки прерванного этапа — в output."))
	if st.LogTail != "" {
		ce.Details = map[string]any{"output": st.LogTail}
	}
	return ce
}
