// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package agentconf

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The markers of the evo section of a Markdown file the user keeps
// (AGENTS.md): the section is found by them, and only it is replaced. A
// begin marker is any line starting with SectionBeginPrefix, so its wording
// may change between versions.
const (
	SectionBeginPrefix = "<!-- BEGIN evo"
	sectionBegin       = SectionBeginPrefix + ": written by 'evo agent setup --skill', replaced on the next run -->"
	sectionEnd         = "<!-- END evo -->"
)

// Section puts body (text ending with a line break) between the evo
// markers of the Markdown file old: a new section goes at the end of the
// file, after a blank line; a section already there is replaced, the text
// around it kept.
func Section(old []byte, body string) (Result, error) {
	nl := newline(old)
	text := strings.ReplaceAll(sectionBegin+"\n"+body+sectionEnd+"\n", "\n", nl)
	var begins, ends [][2]int // line start, line end (after its break)
	for start := 0; start < len(old); {
		end := len(old)
		if i := bytes.IndexByte(old[start:], '\n'); i >= 0 {
			end = start + i + 1
		}
		switch line := strings.TrimSpace(string(old[start:end])); {
		case strings.HasPrefix(line, SectionBeginPrefix):
			begins = append(begins, [2]int{start, end})
		case line == sectionEnd:
			ends = append(ends, [2]int{start, end})
		}
		start = end
	}
	switch {
	case len(begins) == 0 && len(ends) == 0:
		if len(bytes.TrimSpace(old)) == 0 {
			return Result{Data: []byte(text), State: Added}, nil
		}
		sep := nl
		if !bytes.HasSuffix(old, []byte("\n")) {
			sep = nl + nl
		}
		return Result{Data: splice(old, len(old), len(old), sep+text), State: Added}, nil
	case len(begins) == 1 && len(ends) == 1 && begins[0][0] < ends[0][0]:
		b, e := begins[0], ends[0]
		if bytes.Equal(normalizeNewlines(old[b[1]:e[0]]), []byte(body)) {
			return Result{Data: old, State: Same}, nil
		}
		return Result{Data: splice(old, b[0], e[1], text), State: Replaced}, nil
	}
	var at int // the line that breaks the pair
	switch {
	case len(begins) > 1:
		at = begins[1][0]
	case len(ends) > 1:
		at = ends[1][0]
	case len(begins) == 1:
		at = begins[0][0]
	default:
		at = ends[0][0]
	}
	return Result{}, errorAt(old, at, i18n.Tf("the evo section is broken: the file needs one %q line and one %q line after it",
		"секция evo повреждена: в файле нужны одна строка %q и после неё одна строка %q", SectionBeginPrefix+" …", sectionEnd))
}

// Whole is a file that is all evo's (SKILL.md): content in place of old.
func Whole(old, content []byte) Result {
	switch {
	case len(old) == 0:
		return Result{Data: content, State: Added}
	case bytes.Equal(normalizeNewlines(old), normalizeNewlines(content)):
		return Result{Data: old, State: Same}
	}
	return Result{Data: content, State: Replaced}
}

// diffContext is the number of unchanged lines around a change.
const diffContext = 3

// Diff is a unified diff from old to new of the file path ("/dev/null"
// for old when it is empty), "" lines when they are equal. An edit of this
// package changes one place of a file, so the diff is one hunk: from the
// first line that differs to the last one.
func Diff(path string, old, new []byte) []string {
	a, b := lines(old), lines(new)
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	if pre == len(a) && pre == len(b) {
		return nil
	}
	from := max(pre-diffContext, 0)
	toA, toB := min(len(a)-suf+diffContext, len(a)), min(len(b)-suf+diffContext, len(b))
	oldName := path
	if len(old) == 0 {
		oldName = "/dev/null"
	}
	out := []string{"--- " + oldName, "+++ " + path,
		fmt.Sprintf("@@ -%s +%s @@", hunkRange(from, toA-from), hunkRange(from, toB-from))}
	for _, l := range a[from:pre] {
		out = append(out, " "+l)
	}
	for _, l := range a[pre : len(a)-suf] {
		out = append(out, "-"+l)
	}
	for _, l := range b[pre : len(b)-suf] {
		out = append(out, "+"+l)
	}
	for _, l := range a[len(a)-suf : toA] {
		out = append(out, " "+l)
	}
	return out
}

// hunkRange is "start,count" of a unified diff hunk (start 1-based; the
// line before the hunk when it is empty).
func hunkRange(from, n int) string {
	if n == 0 {
		return fmt.Sprintf("%d,0", from)
	}
	return fmt.Sprintf("%d,%d", from+1, n)
}

// lines splits data into lines without their breaks.
func lines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := strings.TrimSuffix(string(normalizeNewlines(data)), "\n")
	return strings.Split(s, "\n")
}
