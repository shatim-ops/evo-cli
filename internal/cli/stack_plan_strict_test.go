// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/stack"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// D108 («Риск», conclusion 186, У6): `evo stack plan` from a file and MCP
// stack_plan give one refusal of a manifest with unknown fields — USAGE,
// exit code 1, the same list of paths.
func TestStackPlan_UnknownFieldsSameInCLIAndMCP(t *testing.T) {
	withParityWorld(t)
	manifest := `{"name":"s","_comment":"a note","vm":{"name":"v","pubic_ip":true}}`
	file := filepath.Join(t.TempDir(), "stack.json")
	if err := os.WriteFile(file, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	newProcess()
	code, _, stderr := runCLI(t, "", "--json", "stack", "plan", "--manifest", file)
	env, ok := errorEnvelope(stderr)
	if !ok || code != clerrors.ExitCode(clerrors.New(clerrors.CodeUsage, "", "")) || env.Reason != clerrors.CodeUsage {
		t.Fatalf("want USAGE and its exit code, got exit %d: %s", code, stderr)
	}
	tl, _ := newRegistry().Lookup("stack_plan")
	var m map[string]any
	_ = json.Unmarshal([]byte(manifest), &m)
	text, _ := tl.Handler(t.Context(), map[string]any{"manifest": m})
	var answer map[string]any
	_ = json.Unmarshal([]byte(text), &answer)
	if !reflect.DeepEqual(env.Details["unknown_fields"], answer["unknown_fields"]) || answer["unknown_fields"] == nil {
		t.Errorf("the command and the tool name other paths:\n%v\n%v", env.Details["unknown_fields"], answer["unknown_fields"])
	}
}

func TestLBStackHumanQuestion(t *testing.T) {
	for _, lang := range []string{"en", "ru"} {
		t.Run(lang, func(t *testing.T) {
			_, requests := withJSONWorld(t)
			t.Setenv("EVO_LANG", lang)
			oldTTY, oldIn := confirm.StdinIsTTY, confirm.Stdin
			t.Cleanup(func() { confirm.StdinIsTTY, confirm.Stdin = oldTTY, oldIn })
			confirm.StdinIsTTY = func() bool { return true }
			var m stack.Manifest
			if err := json.Unmarshal([]byte(`{"name":"test","vm":{"name":"vm"},"nlb":{"name":"web","port":80}}`), &m); err != nil {
				t.Fatal(err)
			}
			j := stack.Plan(m)
			j.ProjectID = pvProject
			const subnet = "00000000-0000-0000-0000-0000000000d1"
			j.LBPlacement = &stack.LBPlacement{Project: pvProject, VPC: parityVM, Zone: pvZone, Subnet: subnet, Name: "web", Port: 80, TargetPort: 80}
			for _, paused := range []bool{false, true} {
				if paused {
					j.Status = "awaiting_lb_approval"
					j.LBPending = map[string]any{"name": "web"}
					j.LBUncertain = true
					for _, st := range j.Steps {
						if st.Key != "lb" {
							st.Status = "done"
							st.ResourceID = parityVM
							j.Outputs[st.Key] = parityVM
						} else {
							st.Args["vpc_id"] = parityVM
							st.Args["zone"] = pvZone
						}
					}
				}
				if err := j.Save(); err != nil {
					t.Fatal(err)
				}
				for _, flags := range [][]string{{"--dry-run"}, {"--json", "--dry-run"}, {}} {
					confirm.Stdin = strings.NewReader("n\n")
					args := append(append([]string{}, flags...), "stack", "apply", j.ID)
					code, out, stderr := runCLI(t, "", args...)
					shown := out + stderr
					want := 0
					if len(flags) == 0 {
						want = 3
					}
					if code != want || !strings.Contains(shown, pvProject) || !strings.Contains(shown, parityVM) || !strings.Contains(shown, subnet) || !strings.Contains(shown, pvZone) {
						t.Fatalf("paused=%t %v code=%d %s", paused, flags, code, shown)
					}
					if paused && !strings.Contains(shown, "already be billed") && !strings.Contains(shown, "могло привести к оплате") {
						t.Fatal(shown)
					}
					if writes := requests.takeWrites(); len(writes) != 0 {
						t.Fatal(writes)
					}
				}
			}
		})
	}
}
