// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// G140-5: every way a step ends in error names its class, not only the
// refusals of its tool: a cluster the cloud left in a terminal status is
// the cloud's failure (API_ERROR), a name a resource already has is the
// manifest's (USAGE, as a create refuses it, D28). One point ends a step
// in error (failStep), so a new way to fail cannot leave error_reasons
// without it.
func TestStackStatus_EveryFailedStepNamesItsReason(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(r *Registry)
		want  string
	}{
		"cluster in a terminal status": {func(r *Registry) {
			r.handlers["pg_list"] = fakeOK(map[string]any{"clusters": []any{}})
			r.handlers["pg_create"] = fakeOK(map[string]any{"status": "creating", "cluster_id": "pg-1", "operation_id": "op"})
			r.handlers["pg_get"] = fakeOK(map[string]any{"status": "FAILED"})
		}, clierrors.CodeAPIError},
		"name already taken": {func(r *Registry) {
			r.handlers["pg_list"] = fakeOK(map[string]any{"clusters": []any{map[string]any{"id": "pg-other", "name": "p"}}})
		}, clierrors.CodeUsage},
	} {
		t.Run(name, func(t *testing.T) {
			stackHome(t)
			r := newTestServer()
			j := planJob(t, `{"name":"s","postgres":{"name":"p"}}`)
			tc.setup(r)
			r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
			var text string
			for range 3 {
				text, _ = r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
			}
			var ans struct {
				Status       string            `json:"status"`
				ErrorReasons map[string]string `json:"error_reasons"`
			}
			if err := json.Unmarshal([]byte(text), &ans); err != nil {
				t.Fatal(err)
			}
			if ans.Status != "error" {
				t.Fatalf("job is %q, want error: the case proves nothing: %s", ans.Status, text)
			}
			if len(ans.ErrorReasons) != 1 {
				t.Fatalf("error_reasons %v, want one step: %s", ans.ErrorReasons, text)
			}
			for step, got := range ans.ErrorReasons {
				if got != tc.want {
					t.Errorf("error_reasons[%s] = %q, want %s: %s", step, got, tc.want, text)
				}
			}
		})
	}
}
