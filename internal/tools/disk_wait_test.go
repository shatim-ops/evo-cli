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

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// Grok live run on v1.3.0, 26/27.10 (N3): disk_attach right after
// disk_create waited 2 minutes for the new disk, and the disk became
// available after 149 s — the attach failed with 422 on a disk that was
// about to be ready. The wait covers what the platform takes (5 minutes),
// and while it waits it says the disk's state (CLAUDE.md §6, axis 2).
func TestDiskWait_CoversLiveCreateTime(t *testing.T) {
	const liveCreate = 149 * time.Second
	if diskReadyTimeout < 2*liveCreate {
		t.Errorf("diskReadyTimeout %s is under twice the live create time %s", diskReadyTimeout, liveCreate)
	}
}

// slowDiskCloud lists the disk creating for the first polls, then
// available.
type slowDiskCloud struct {
	cancelCloud
	mu    sync.Mutex
	polls int
}

func (c *slowDiskCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.polls++
	state := "creating"
	if c.polls > 3 {
		state = "available"
	}
	return []map[string]any{{"id": "d-1", "state": state}}, nil
}

func TestDiskWait_SaysTheStateWhileWaiting(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	ctx := WithProgress(context.Background(), func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() })
	eng := &engine.Engine{Cloud: &slowDiskCloud{}, Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: time.Second}}
	if err := waitDiskAvailable(ctx, eng, "d-1", "p"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "creating") || !strings.Contains(joined, "d-1") {
		t.Errorf("the wait must say the disk's state as it goes: %q", joined)
	}
	if n := strings.Count(joined, "creating"); n != 1 {
		t.Errorf("a state is said once, when it changes: %d lines with creating", n)
	}
}
