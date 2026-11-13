// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

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
