// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"strings"
	"testing"
)

// On a terminal the progress line is redrawn in place, padded over a longer
// previous one, and erased before the next line of any kind.
func TestProgress_Terminal(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeText, false)
	p.Progress("waiting… 10s (creating)")
	p.Progress("waiting… 11s")
	p.StepDone("done")
	want := "\r    waiting… 10s (creating)" + "\r    waiting… 11s" + strings.Repeat(" ", 11) + "\r" + "                " + "\r" + "    done\n"
	if got := buf.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	// Nothing to erase any more.
	buf.Reset()
	p.ProgressDone()
	p.Warning("w")
	if got := buf.String(); got != "Warning: w\n" {
		t.Errorf("after done: %q", got)
	}
}

// Without a terminal and in --json there is no progress line at all: the
// caller prints StepDone lines instead.
func TestProgress_NotATerminal(t *testing.T) {
	for _, mode := range []Mode{ModePlain, ModeJSON} {
		var buf bytes.Buffer
		p := NewWithWriter(&buf, mode, false)
		if p.Live() {
			t.Errorf("mode %d: Live", mode)
		}
		p.Progress("waiting… 1s")
		p.ProgressDone()
		if buf.Len() != 0 {
			t.Errorf("mode %d: %q", mode, buf.String())
		}
	}
}

// A state from the cloud cannot move the cursor: control characters go.
func TestProgress_Sanitized(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeText, false)
	p.Progress("state \x1b[2Jx\n")
	if got := buf.String(); got != "\r    state ?[2Jx " {
		t.Errorf("%q", got)
	}
}
