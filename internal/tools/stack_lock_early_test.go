// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// D115: what the job on disk already says is answered at once, though
// another evo process holds the job's lock — the answer the call would
// give once the lock is free, not USAGE after a minute of waiting for it
// (v1.8.2). A destroyed job: stack_destroy answers already_destroyed (D26);
// no job: NOT_FOUND; a destroyed job to apply, or a job of another
// project: USAGE with its own reason. «Миграция» 1.9 (Астра, #2080).
func TestStackLockHeld_KnownOutcomeAnswersAtOnce(t *testing.T) {
	stackHome(t)
	r := New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{Config: config.Config{ProjectID: "project-here"}}, nil
	})
	r.SetStateDir(t.TempDir())
	for _, c := range []struct {
		tool, id, status, project string
		saved                     bool
		wantErr                   bool
		want                      string // status of a success, reason of a refusal
	}{
		{"stack_destroy", "stk-0000000001", stack.StatusDestroyed, "", true, false, "already_destroyed"},
		{"stack_apply", "stk-0000000002", stack.StatusDestroyed, "", true, true, clierrors.CodeUsage},
		{"stack_apply", "stk-0000000003", "running", "project-other", true, true, clierrors.CodeUsage},
		{"stack_destroy", "stk-0000000004", "running", "project-other", true, true, clierrors.CodeUsage},
		{"stack_apply", "stk-0000000005", "", "", false, true, clierrors.CodeNotFound},
		{"stack_destroy", "stk-0000000006", "", "", false, true, clierrors.CodeNotFound},
	} {
		if c.saved {
			job := &stack.Job{ID: c.id, Status: c.status, ProjectID: c.project, Outputs: map[string]string{}}
			if err := job.Save(); err != nil {
				t.Fatal(err)
			}
		}
		// Another evo process holds the lock for the whole call.
		unlock, err := stack.LockJob(c.id, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		var m map[string]any
		var text string
		var isErr bool
		go func() {
			m, text, isErr = callNamed(t, r, c.tool, map[string]any{"job_id": c.id, "confirmed": true})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			unlock()
			t.Fatalf("%s %s: the call waits for the lock another process holds; want the outcome at once", c.tool, c.id)
		}
		unlock()
		got := m["status"]
		if c.wantErr {
			got = m["reason"]
		}
		if isErr != c.wantErr || got != c.want {
			t.Errorf("%s %s (status %q, project %q, saved %v): isError %v, %v; want isError %v, %s: %s",
				c.tool, c.id, c.status, c.project, c.saved, isErr, got, c.wantErr, c.want, text)
		}
	}
}
