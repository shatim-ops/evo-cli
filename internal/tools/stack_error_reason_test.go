// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// G140-5: a step that ends in error tells its class, not only its text.
// stack_status names the reason of each failed step in error_reasons
// (step → the reason its tool answered, a code of internal/errors), beside
// details and attempts, so an agent tells a refusal it can fix (USAGE,
// CONFIG_INVALID) from a missing resource and from the cloud's failure
// without reading the message — in any language of the CLI. The refusals
// here end the step at once; API_ERROR, which the step retries with its
// production pause, is the same answer after the retries.
func TestStackStatus_FailedStepNamesItsReason(t *testing.T) {
	for _, code := range []string{clierrors.CodeUsage, clierrors.CodeConfigInvalid, clierrors.CodeNotFound} {
		t.Run(code, func(t *testing.T) {
			stackHome(t)
			r := newTestServer()
			j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
			r.handlers["provision"] = func(context.Context, map[string]any) (string, bool) {
				return toJSON(map[string]any{"error": "the step's tool refused", "reason": code}), true
			}
			r.handlers["vm_list"] = fakeOK(map[string]any{"vms": []any{}})
			r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
			var text string
			for range 4 {
				text, _ = r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
			}
			loaded, err := stack.LoadJob(j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Steps[0].Status != "error" {
				t.Fatalf("step vm is %q, want error: the case proves nothing", loaded.Steps[0].Status)
			}
			var ans struct {
				ErrorReasons map[string]string `json:"error_reasons"`
			}
			if err := json.Unmarshal([]byte(text), &ans); err != nil {
				t.Fatalf("stack_status answer is not JSON: %v", err)
			}
			if got := ans.ErrorReasons["vm"]; got != code {
				t.Errorf("stack_status: error_reasons[vm] = %q, want %s (the reason provision answered): %s", got, code, text)
			}
		})
	}
}
