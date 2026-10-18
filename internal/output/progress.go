// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Live reports whether a progress line can be redrawn in place: text mode,
// stdout is a terminal. A pipe, a file and --json cannot.
func (p *Printer) Live() bool { return p.mode == ModeText }

// Progress shows detail as a step line ("    detail") that the next
// Progress redraws in place: on a terminal only, with a carriage return and
// spaces, no escape codes. In other modes it prints nothing: there the
// caller prints a StepDone line now and then instead. Any other output of
// the printer ends the line first, so nothing is written over it.
func (p *Printer) Progress(detail string) {
	if !p.Live() {
		return
	}
	line := "    " + sanitizeCell(detail)
	n := utf8.RuneCountInString(line)
	pad := ""
	if p.progressLen > n {
		pad = strings.Repeat(" ", p.progressLen-n)
	}
	if p.color {
		fmt.Fprintf(p.out, "\r%s%s%s%s", colorDim, line, colorReset, pad)
	} else {
		fmt.Fprintf(p.out, "\r%s%s", line, pad)
	}
	p.progressLen = n
}

// ProgressDone erases the progress line, if there is one, and leaves the
// cursor at the start of the empty line.
func (p *Printer) ProgressDone() {
	if p.progressLen == 0 {
		return
	}
	fmt.Fprintf(p.out, "\r%s\r", strings.Repeat(" ", p.progressLen))
	p.progressLen = 0
}
