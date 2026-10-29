// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// composeTailLines is how many of the last lines of docker compose up a
// failure carries (release plan 1.6, #558): the error is there, the pull
// progress before it is not.
const composeTailLines = 20

// tailLineMax bounds one kept line: a pull progress bar can be one line of
// kilobytes.
const tailLineMax = 500

// tailWriter keeps the last n lines written to it.
type tailWriter struct {
	mu    sync.Mutex
	n     int
	lines []string
	part  []byte
}

func newTailWriter(n int) *tailWriter { return &tailWriter{n: n} }

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.part = append(w.part, p...)
	for {
		i := strings.IndexByte(string(w.part), '\n')
		if i < 0 {
			break
		}
		w.push(string(w.part[:i]))
		w.part = w.part[i+1:]
	}
	// A progress bar rewrites one line with \r and no \n for a whole pull:
	// the unfinished line keeps its last part only (review of #566).
	if len(w.part) > 2*tailLineMax {
		w.part = []byte(lastBytes(string(w.part), tailLineMax))
	}
	return len(p), nil
}

func (w *tailWriter) push(line string) {
	line = strings.TrimRight(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:] // what a terminal shows of a line rewritten with \r
	}
	if len(line) > tailLineMax {
		line = firstBytes(line, tailLineMax) + "…"
	}
	w.lines = append(w.lines, line)
	if len(w.lines) > w.n {
		w.lines = w.lines[len(w.lines)-w.n:]
	}
}

// String is the kept lines, the last unfinished one included.
func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	lines := w.lines
	part := strings.TrimRight(string(w.part), "\r")
	if i := strings.LastIndexByte(part, '\r'); i >= 0 {
		part = part[i+1:]
	}
	if part != "" {
		lines = append(append([]string(nil), lines...), part)
		if len(lines) > w.n {
			lines = lines[len(lines)-w.n:]
		}
	}
	return strings.Join(lines, "\n")
}

// firstBytes is at most n bytes of the start of s, cut at a rune boundary.
func firstBytes(s string, n int) string {
	for n > 0 && n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// lastBytes is at most n bytes of the end of s, cut at a rune boundary.
func lastBytes(s string, n int) string {
	i := len(s) - n
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}
