// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// An answer of a create still being made at the limit names when evo last
// read the resource and how many reads failed after it, as every wait at
// its limit does (waitReads, #2216): a «creating» read once and then never
// again is not a fresh one (Астра #2317, after #2228).
func TestMadeAnswer_AtTheLimitSaysTheReadsThatFailed(t *testing.T) {
	const id = "00000000-0000-4000-8000-0000000000f1"
	for _, base := range []made{subnetMade, snatMadeNotes()} {
		t.Run(base.tool, func(t *testing.T) {
			n := 0
			m := base
			m.read = func(context.Context) ([]map[string]any, error) {
				n++
				if n == 1 {
					return []map[string]any{{"id": id, "state": "creating"}}, nil
				}
				return nil, errors.New("synthetic unavailable")
			}
			eng := &engine.Engine{Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: 30 * time.Millisecond}}
			text, isErr := madeAnswer(context.Background(), eng, map[string]any{m.field: map[string]any{"id": id, "state": "creating"}}, m)
			var ans map[string]any
			if err := json.Unmarshal([]byte(text), &ans); err != nil {
				t.Fatal(err)
			}
			if n < 2 || isErr || ans["status"] != "creating" {
				t.Fatalf("the wait did not reach its limit after failed reads: reads %d, %s", n, text)
			}
			if ans["last_read_at"] == nil || ans["failed_reads"] != float64(n-1) {
				t.Errorf("reads %d: the answer at the limit does not say when it read last and how many reads failed: %s", n, text)
			}
		})
	}
}
