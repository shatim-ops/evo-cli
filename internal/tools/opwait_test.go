// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOpTerminal(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]any
		want bool
	}{
		{"done-true", map[string]any{"done": true}, true},
		{"done-false", map[string]any{"done": false}, false},
		{"status-done", map[string]any{"status": "DONE"}, true},
		{"status-ready-lower", map[string]any{"status": "ready"}, true},
		{"state-failed", map[string]any{"state": "FAILED"}, true},
		{"status-provisioning", map[string]any{"status": "PROVISIONING"}, false},
		// An operation that is RUNNING is in progress, not finished.
		{"status-running", map[string]any{"status": "RUNNING"}, false},
		{"running-but-done", map[string]any{"status": "RUNNING", "done": true}, true},
		{"error-nonempty", map[string]any{"error": "boom"}, true},
		{"error-empty-string", map[string]any{"error": ""}, false},
		{"error-null", map[string]any{"error": nil}, false},
		{"empty", map[string]any{}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := opTerminal(c.m); got != c.want {
			t.Errorf("%s: opTerminal=%v want %v", c.name, got, c.want)
		}
	}
}

func TestWaitForOpJSON_StopsOnTerminal(t *testing.T) {
	defer func(d time.Duration) { opWaitFirstDelay = d }(opWaitFirstDelay)
	opWaitFirstDelay = time.Millisecond
	calls := 0
	fetch := func(ctx context.Context) (map[string]any, error) {
		calls++
		if calls >= 2 {
			return map[string]any{"done": true}, nil
		}
		return map[string]any{"status": "PENDING"}, nil
	}
	start := time.Now()
	m, err := waitForOpJSON(context.Background(), 60, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if !opTerminal(m) {
		t.Errorf("expected terminal, got %v", m)
	}
	if calls != 2 {
		t.Errorf("expected 2 fetches, got %d", calls)
	}
	// wall-clock: guards waiting out wait_seconds (60 s): half of it.
	if time.Since(start) > 30*time.Second {
		t.Errorf("took too long: %v", time.Since(start))
	}
}

func TestWaitForOpJSON_ReturnsErr(t *testing.T) {
	want := errors.New("net down")
	_, err := waitForOpJSON(context.Background(), 30, func(ctx context.Context) (map[string]any, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Errorf("expected err propagated, got %v", err)
	}
}

func TestWaitForOpJSON_ZeroWaitSingleFetch(t *testing.T) {
	calls := 0
	_, _ = waitForOpJSON(context.Background(), 0, func(ctx context.Context) (map[string]any, error) {
		calls++
		return map[string]any{"status": "PENDING"}, nil
	})
	if calls != 1 {
		t.Errorf("zero wait must fetch exactly once, got %d", calls)
	}
}

// Повторная приемка #90, N-08: ожидание истекло, операция не завершена —
// ответ говорит «еще идет», а не только done=false.
func TestWaitForOpJSON_DeadlineMarksStillRunning(t *testing.T) {
	defer func(d time.Duration) { opWaitFirstDelay = d }(opWaitFirstDelay)
	opWaitFirstDelay = 50 * time.Millisecond
	m, err := waitForOpJSON(context.Background(), 1, func(ctx context.Context) (map[string]any, error) {
		return map[string]any{"done": false}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if m["still_running"] != true || m["done"] != false {
		t.Errorf("deadline answer = %v, want done=false and still_running=true", m)
	}
}
