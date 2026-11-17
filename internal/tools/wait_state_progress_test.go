// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
)

// A resource that stays in one state for the whole wait is not a silent
// wait: after the line of the state, a line with the time waited goes out
// every progress interval, in the CLI and over MCP alike (CLAUDE.md §6,
// axis 2; plan 1.9, «ход ожидания без смены состояния»). The time is a
// reading of internal/clock, so the test moves it, not the wall.
func TestWaitState_SaysTheTimeWhileTheStateStays(t *testing.T) {
	old := clock.Now
	var cmu sync.Mutex
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock.Now = func() time.Time {
		cmu.Lock()
		defer cmu.Unlock()
		now = now.Add(20 * time.Second) // each reading: 20 s of a long wait
		return now
	}
	t.Cleanup(func() { clock.Now = old })

	var mu sync.Mutex
	var lines []string
	ctx := WithProgress(context.Background(), func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() })
	eng := &engine.Engine{Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: time.Second, VMProgressInterval: 30 * time.Second}}
	polls := 0
	last, _, _, err := waitState(ctx, eng, time.Second,
		func(context.Context) (string, bool, error) {
			polls++
			if polls > 8 {
				return "ready", true, nil
			}
			return "creating", true, nil
		},
		func(st string) string { return "disk d-1: " + st + ", waiting…" },
		func(st string) (bool, error) { return st == "ready", nil })
	if err != nil || last != "ready" {
		t.Fatalf("waitState: %q, %v", last, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(lines) < 3 {
		t.Fatalf("one line for the state and then lines with the time waited, got %d: %q", len(lines), lines)
	}
	if strings.Contains(lines[0], "so far") {
		t.Errorf("the first line is the state, as before: %q", lines[0])
	}
	for _, l := range lines[1:] {
		if !strings.Contains(l, "disk d-1: creating") || !strings.Contains(l, "so far") {
			t.Errorf("a repeated line names the state and the time waited: %q", l)
		}
	}
}
