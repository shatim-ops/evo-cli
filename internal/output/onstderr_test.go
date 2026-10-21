// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"strings"
	"testing"
)

// OnStderr writes everything to the error writer; it is live only when
// that writer is a terminal (stdout does not matter), and --json stays
// quiet. A step of one has no counter.
func TestOnStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	p := NewWithWriters(&out, &errOut, ModeText, false)
	e := p.OnStderr()
	if e.Live() {
		t.Error("live on an error writer that is not a terminal")
	}
	e.Step(0, 0, "Creating VM web…")
	e.StepDone("waiting… 1s (creating)")
	e.Success("ready")
	if out.Len() != 0 || errOut.String() != "==> Creating VM web…\n    waiting… 1s (creating)\nready\n" {
		t.Errorf("stdout %q, stderr %q", out.String(), errOut.String())
	}

	errOut.Reset()
	p.errLive = true // stderr is a terminal, stdout is a pipe
	p.mode = ModePlain
	e = p.OnStderr()
	if !e.Live() {
		t.Error("not live on a terminal stderr")
	}
	e.Progress("waiting… 2s")
	e.ProgressDone()
	if errOut.String() != "\r    waiting… 2s\r"+strings.Repeat(" ", 15)+"\r" || out.Len() != 0 {
		t.Errorf("stderr %q", errOut.String())
	}

	errOut.Reset()
	j := NewWithWriters(&out, &errOut, ModeJSON, false)
	j.errLive = true
	q := j.OnStderr()
	q.Step(0, 0, "x")
	q.Progress("y")
	q.Success("z")
	if q.Live() || out.Len()+errOut.Len() != 0 {
		t.Errorf("--json printed: %q %q", out.String(), errOut.String())
	}
}
