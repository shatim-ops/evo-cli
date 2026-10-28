// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// CLAUDE.md §6, axis 2 (#457): an engine's line during a call (the state
// of a VM or a floating IP it waits for) goes to the log the answer
// carries and, to an MCP client that asked for progress, as it is
// printed — not only in the answer at the end.
func TestLogAndProgress_LineReachesBoth(t *testing.T) {
	var log bytes.Buffer
	var sent []string
	ctx := WithProgress(context.Background(), func(line string) { sent = append(sent, line) })
	logAndProgress(ctx, &log).Step(0, 0, "VM vm-1: deleting, waiting until it is deleted…")
	if !strings.Contains(log.String(), "vm-1: deleting") || len(sent) != 1 || !strings.Contains(sent[0], "vm-1: deleting") {
		t.Errorf("log %q, progress %q: want the line in both", log.String(), sent)
	}

	// Nobody watches: the log alone.
	log.Reset()
	logAndProgress(context.Background(), &log).Step(0, 0, "line")
	if !strings.Contains(log.String(), "line") {
		t.Errorf("log %q: want the line", log.String())
	}
}
