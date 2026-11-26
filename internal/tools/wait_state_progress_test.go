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
	"github.com/shatim-ops/evo-cli/internal/i18n"
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

	was := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(was) })
	for _, lang := range []string{i18n.RU, i18n.EN} {
		t.Run(lang, func(t *testing.T) {
			i18n.SetLang(lang)
			var lines []string
			ctx := WithProgress(context.Background(), func(line string) { lines = append(lines, line) })
			cloud := &diskCloud{readyAt: 9}
			eng := &engine.Engine{Cloud: cloud, Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: time.Second, VMProgressInterval: 30 * time.Second}}
			last, err := waitDiskAvailable(ctx, eng, cloud.made(), "project")
			if err != nil || last != "available" || len(lines) < 3 {
				t.Fatalf("disk wait: state %q, progress %q, error %v", last, lines, err)
			}
			waiting, elapsed := "waiting until it is available", "so far"
			if lang == i18n.RU {
				waiting, elapsed = "жду, пока он станет доступен", "прошло"
			}
			for i, line := range lines {
				if !strings.Contains(line, waiting) || !strings.Contains(line, cloud.made()) || !strings.Contains(line, "creating") || strings.Contains(line, elapsed) != (i > 0) ||
					answerLanguageIssue(lang, "", "progress", line, nil) != "" {
					t.Errorf("progress line %d: %q", i, line)
				}
			}
		})
	}
}
