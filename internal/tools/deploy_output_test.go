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

// #558: the last lines of docker compose up go with deploy's error
// (engine: CLIError.Details["output"]) into the tool's answer, and deploy's
// log and output are the workload's words — marked untrusted, as the logs
// of logs and ca_logs (process audit 1.2.1, П9). The log of a deploy over
// MCP always carried the output of docker compose up unmarked.
func TestDeploy_ComposeOutputGoesWithTheErrorUntrusted(t *testing.T) {
	ce := clierrors.New(clierrors.CodeSSHTimeout, "docker compose up failed", "")
	ce.Details = map[string]any{"output": "Error response from daemon: pull access denied"}
	fields := deployFields(ce, map[string]any{"log": "==> deploy"})
	if fields["output"] != ce.Details["output"] {
		t.Errorf("fields = %v, want the output of the error", fields)
	}
	if got := deployFields(clierrors.New(clierrors.CodeSSHTimeout, "x", ""), nil); got != nil {
		t.Errorf("an error without output: fields %v, want none", got)
	}
	r := New(nil)
	tl, _ := r.Lookup("deploy")
	if !tl.UntrustedOutput {
		t.Error("deploy is not marked as returning the workload's output")
	}
	h := markUntrusted(untrustedFields["deploy"], func(context.Context, map[string]any) (string, bool) {
		return errAnswer(ce, fields), true
	})
	text, _ := h(context.Background(), nil)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatal(err)
	}
	marked, _ := m["_untrusted"].([]any)
	if len(marked) != 2 || marked[0] != "log" || marked[1] != "output" {
		t.Errorf("_untrusted = %v, want [log output]: %s", m["_untrusted"], text)
	}
}
