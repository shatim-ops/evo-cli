// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// The moment a waiting step first missed its resource goes into the
// answer — the step's detail and the text it ends with — so it is read
// from internal/clock, as every time value of an answer is (CLAUDE.md §5);
// and how long the step has missed is counted on that same clock, from
// that moment, as restoreHeldLimit is: one clock for the value and the
// limit drawn from it (the check of rc.1: time.Now went into the answer
// past internal/clock).
func TestStepMissing_TheMomentIsTheClocks(t *testing.T) {
	stopped := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	old := clock.Now
	clock.Now = func() time.Time { return stopped }
	t.Cleanup(func() { clock.Now = old })

	job := &stack.Job{}
	st := &stack.Step{Key: "postgres", ResourceID: "cluster-1"}
	for i := 1; i < maxStepMissing; i++ {
		if stepMissing(job, st, "pg_get: not found", "pg_get", clierrors.CodeNotFound) {
			t.Fatalf("miss %d ended the step: %s", i, st.Detail)
		}
	}
	if !st.MissingSince.Equal(stopped) || !strings.Contains(st.Detail, "2026-11-17T12:00:00Z") {
		t.Errorf("MissingSince %v, detail %q; want the moment of internal/clock, %v", st.MissingSince, st.Detail, stopped)
	}
	// The misses reach maxStepMissing while the clock stands: not yet
	// stepMissingFor since the first, the step waits.
	if stepMissing(job, st, "pg_get: not found", "pg_get", clierrors.CodeNotFound) {
		t.Fatalf("the step ended at its first miss's moment, before stepMissingFor: %s", st.Detail)
	}
	// The clock moves past stepMissingFor: the step ends, and the text it
	// ends with names the same moment.
	clock.Now = func() time.Time { return stopped.Add(stepMissingFor) }
	if !stepMissing(job, st, "pg_get: not found", "pg_get", clierrors.CodeNotFound) {
		t.Fatalf("stepMissingFor passed on the clock and the step still waits: %s", st.Detail)
	}
	if !strings.Contains(st.Detail, "2026-11-17T12:00:00Z") {
		t.Errorf("the step ended with %q; want the moment of the first miss by internal/clock", st.Detail)
	}
}
