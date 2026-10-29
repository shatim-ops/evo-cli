// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	stderrors "errors"
	"fmt"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
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
