// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// #558: an engine command's --json error is the engine's error with what
// the tool's answer adds — access_denied as before, and now the fields the
// answer marks untrusted, with the marker: evo deploy --json carries the
// last lines of docker compose up as the MCP answer does, marked as the
// workload's words (process audit 1.2.1, П9).
func TestEngineError_CarriesTheAnswersUntrustedFields(t *testing.T) {
	ce := clerrors.New(clerrors.CodeSSHTimeout, "docker compose up failed", "")
	ce.Details = map[string]any{"output": "pull access denied"}
	text := `{"status":"error","reason":"SSH_TIMEOUT","log":"==> deploy","output":"pull access denied","_untrusted":["log","output"]}`
	err := withAnswerDetails(ce, text)
	var got *clerrors.CLIError
	if !stderrors.As(err, &got) {
		t.Fatalf("err = %v", err)
	}
	if fmt.Sprint(got.Details["_untrusted"]) != "[log output]" || got.Details["output"] != "pull access denied" || got.Details["log"] != "==> deploy" {
		t.Errorf("details = %v", got.Details)
	}
	if got.Message != "docker compose up failed" {
		t.Errorf("message = %q: the engine's own error stays", got.Message)
	}
}

// The marker "_untrusted" is for a client of the JSON: the table a person
// reads has no line of it — a list stays one table, a resource has no
// FIELD _untrusted — and the delete's question has no line of it either.
func TestRenderAnswer_NoMarkerForAPerson(t *testing.T) {
	for _, text := range []string{
		`{"clusters":[{"id":"a","name":"web","status":"RUNNING"}],"_untrusted":["clusters"]}`,
		`{"id":"a","name":"web","status":"RUNNING","access":"x","_untrusted":["id","name","status"]}`,
	} {
		var b bytes.Buffer
		if err := renderAnswer(output.NewWithWriter(&b, output.ModeText, false), &b, text, tableView{}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "_untrusted") || !strings.Contains(b.String(), "web") {
			t.Errorf("%s:\n%s", text, b.String())
		}
	}
	p := toolPreview("k8s_delete", map[string]any{"status": "confirmation_required", "message": "deletes",
		"target": map[string]any{"id": "a", "name": "web"}, "_untrusted": []any{"target"}})
	if lines := strings.Join(p.Lines, "\n"); strings.Contains(lines, "_untrusted") || !strings.Contains(lines, "web") {
		t.Errorf("the question of a delete:\n%s", lines)
	}
}
