// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// Release plan 1.6 (#558; Fable on the Grok wave of rc.1, low): with --json
// the output of docker compose up went nowhere, and its failure said only
// «docker compose up failed». The error carries the last lines of that
// output in its details (output) — the workload's words, which the tool
// marks untrusted — in every mode; the lines before them are not kept.
func TestDeploy_ComposeUpFailureCarriesItsLastLines(t *testing.T) {
	for _, mode := range []output.Mode{output.ModeJSON, output.ModePlain} {
		dep := newMockDeployer()
		upCmd := "cd /opt/app && docker compose -f docker-compose.yml up -d"
		var lines []string
		for i := 1; i <= composeTailLines+5; i++ {
			lines = append(lines, fmt.Sprintf("line %d", i))
		}
		lines = append(lines, "Error response from daemon: pull access denied for app")
		dep.streamOut = func(cmd string) string {
			if cmd == upCmd {
				return strings.Join(lines, "\n") + "\n"
			}
			return ""
		}
		dep.streamErrs = map[string][]error{upCmd: {stderrors.New("Process exited with status 1")}}
		eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
		var buf bytes.Buffer
		eng.Printer = output.NewWithWriter(&buf, mode, false)
		err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
		var ce *clerrors.CLIError
		if !stderrors.As(err, &ce) {
			t.Fatalf("mode %v: Deploy = %v", mode, err)
		}
		out, _ := ce.Details["output"].(string)
		if !strings.Contains(out, "pull access denied") || strings.Contains(out, "line 1\n") || strings.Count(out, "\n") > composeTailLines {
			t.Errorf("mode %v: output = %q, want the last %d lines", mode, out, composeTailLines)
		}
	}
}
