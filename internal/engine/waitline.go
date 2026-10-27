// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// WaitLine shows a wait of minutes as it goes, so that a slow cloud is told
// apart from a stuck command: "waiting for the VM… 1m20s (creating)". On a
// terminal one line is redrawn every second (Printer.Progress); without
// one a line goes out on every change of the state and every interval;
// --json prints nothing. The caller selects on C and calls Show, reports
// the states it polls with State, and calls Done on every way out: Done
// erases the line, so that the next output does not land on it.
type WaitLine struct {
	p     *output.Printer
	text  func(elapsed, state string) string
	start time.Time
	state string
	tick  *time.Ticker
}

// NewWaitLine starts a wait shown on p as text(elapsed, state). every is
// how often a line goes out without a terminal (0: the default of
// Timings.VMProgressInterval).
func NewWaitLine(p *output.Printer, every time.Duration, text func(elapsed, state string) string) *WaitLine {
	if every <= 0 {
		every = vmProgressInterval
	}
	if p.Live() {
		every = vmLiveRefresh
	}
	return &WaitLine{p: p, text: text, start: clock.Now(), tick: time.NewTicker(every)}
}

// VMWaitLine is the wait of a new VM for becoming active, in the words of
// deploy, shown on p.
func (e *Engine) VMWaitLine(p *output.Printer) *WaitLine {
	return NewWaitLine(p, e.timings().VMProgressInterval, func(elapsed, state string) string {
		return i18n.Tf("waiting for the VM… %s (%s)", "ожидание ВМ… %s (%s)", elapsed, statusText(state))
	})
}

// C ticks when the line is due again.
func (w *WaitLine) C() <-chan time.Time { return w.tick.C }

// Show prints the line now.
func (w *WaitLine) Show() {
	msg := w.text(ElapsedText(w.Elapsed()), w.state)
	if w.p.Live() {
		w.p.Progress(msg)
	} else {
		w.p.StepDone(msg)
	}
}

// State shows the line when the state polled differs from the last one.
func (w *WaitLine) State(s string) {
	if s != w.state {
		w.state = s
		w.Show()
	}
}

// Elapsed is the time since the wait started.
func (w *WaitLine) Elapsed() time.Duration { return clock.Since(w.start) }

// Done stops the line and erases it from the terminal. It may be called
// more than once.
func (w *WaitLine) Done() {
	w.tick.Stop()
	w.p.ProgressDone()
}
