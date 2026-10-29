// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"sync"
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
	return len(p), nil
}

func (w *tailWriter) push(line string) {
	line = strings.TrimRight(line, "\r")
	if len(line) > tailLineMax {
		line = line[:tailLineMax] + "…"
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
	if len(w.part) > 0 {
		lines = append(append([]string(nil), lines...), string(w.part))
		if len(lines) > w.n {
			lines = lines[len(lines)-w.n:]
		}
	}
	return strings.Join(lines, "\n")
}
