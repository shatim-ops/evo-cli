// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
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
	last, _, _, _, err := waitState(ctx, eng, time.Second,
		func(context.Context) (string, waitObservation, error) {
			polls++
			if polls > 8 {
				return "ready", waitObserved, nil
			}
			return "creating", waitObserved, nil
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

func TestMadeProgressBeforeVisible(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel bool
		broken bool
	}{
		{"appears", false, false}, {"cancel", true, false}, {"read failures", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := clock.Now
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			clock.Now = func() time.Time { return now }
			t.Cleanup(func() { clock.Now = old })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var lines []string
			ctx = WithProgress(ctx, func(line string) { lines = append(lines, line) })
			polls := 0
			m := subnetMade
			m.read = func(context.Context) ([]map[string]any, error) {
				polls++
				now = now.Add(10 * time.Second)
				if tc.cancel && polls == 7 {
					cancel()
				}
				if polls <= 7 {
					if tc.broken {
						return nil, errors.New("read refused")
					}
					return nil, nil
				}
				return []map[string]any{{"id": "sub", "state": "created"}}, nil
			}
			eng := &engine.Engine{Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: time.Second}}
			answer, failed := madeAnswer(ctx, eng, map[string]any{"subnet": map[string]any{"id": "sub", "state": "creating"}}, m)
			wantPolls := 8
			if tc.cancel {
				wantPolls = 7
			}
			if polls != wantPolls || failed != tc.cancel || (tc.cancel && !strings.Contains(answer, "INTERRUPTED")) || (!tc.cancel && !strings.Contains(answer, `"created"`)) {
				t.Fatalf("reads=%d answer=%s", polls, answer)
			}
			var elapsed []string
			for _, line := range lines {
				if strings.Contains(line, "so far") {
					elapsed = append(elapsed, line)
				}
			}
			if tc.broken {
				for _, line := range elapsed {
					if strings.Contains(line, "not listed") {
						t.Fatal(line)
					}
				}
				return
			}
			if len(elapsed) != 2 || !strings.Contains(elapsed[0], "30s") || !strings.Contains(elapsed[1], "1m") {
				t.Fatalf("elapsed progress=%q reads=%d", elapsed, polls)
			}
		})
	}
}

// A refusal at the read boundary terminates the wait even if a previous
// successful list was empty. It must not produce a heartbeat or another GET.
func TestWaitBeforeVisibleStopsAtReadError(t *testing.T) {
	refusal := errors.New("forbidden")
	var lines []string
	ctx := WithProgress(context.Background(), func(s string) { lines = append(lines, s) })
	eng := &engine.Engine{Timings: engine.Timings{VMPollInterval: time.Millisecond, VMProgressInterval: time.Hour}}
	polls := 0
	_, _, _, reads, err := waitState(ctx, eng, time.Second, func(context.Context) (string, waitObservation, error) {
		polls++
		if polls == 1 {
			return "", waitNotListed, nil
		}
		return "", waitReadFailed, refusal
	}, func(string) string { t.Fatal("no observed state"); return "" }, func(string) (bool, error) { t.Fatal("no observed state"); return false, nil })
	if !errors.Is(err, refusal) || polls != 2 || len(lines) != 0 || reads.failed != 1 || !reads.lastSeen.IsZero() {
		t.Fatal(err, polls, lines, reads)
	}
}
