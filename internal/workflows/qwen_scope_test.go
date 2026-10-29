// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The review of Qwen comes by itself only on a PR that touches the rules of
// the workflows (.github/, internal/workflows/) or the code of secrets and
// consent; /qwen review runs on any; critical, high and secrets, consent and
// workflows are findings beyond the cap of five (owner's decision 29.10,
// audit #578, item 9; before — #485, П1 (б)). ci tests the choice with git
// and gh faked (qwen_review_scope_test.sh), and the script holds it.
func TestWorkflows_QwenReviewScope(t *testing.T) {
	if !runsStep(readWorkflows(t)["ci.yml"], ".github/scripts/qwen_review_scope_test.sh") {
		t.Error("ci: which PRs Qwen reviews is not tested (qwen_review_scope_test.sh)")
	}
	s := qwenScript(t)
	for _, want := range []string{".github/* | internal/workflows/*) review=1", "*_test.go) ;;",
		`"${COMMENT_ID:-}"`, "не больше пяти находок", "«Гипотезы»", "всегда находкой, даже сверх пяти"} {
		if !strings.Contains(s, want) {
			t.Errorf("qwen-review.sh: no %s — the scope or the cap of #485 is gone", want)
		}
	}
}

// sensitiveCode are the functions that hold the policy of secrets and
// consent: the file that defines each is in the scope of the review that
// comes by itself.
var sensitiveCode = map[string]string{
	"internal/tools/gate.go":         "func (r *Registry) gated(",
	"internal/tools/untrusted.go":    "func markUntrusted(",
	"internal/mcp/http_redact.go":    "func redactSecrets(",
	"internal/tools/tools_secret.go": "",
	"internal/mcp/http_policy.go":    "",
	"internal/mcp/http_kube.go":      "",
}

// The scope of the review that comes by itself names the code of secrets
// and consent, and every path of it is there: a file renamed out of the
// list would leave its changes without review.
func TestWorkflows_QwenScopeCoversSensitiveCode(t *testing.T) {
	s := qwenScript(t)
	root := filepath.Join("..", "..")
	for file, fn := range sensitiveCode {
		if !strings.Contains(s, file) {
			t.Errorf("qwen-review.sh: %s, code of secrets or consent, is not in the scope of the review", file)
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Errorf("%s: %v — the scope names a file that is gone", file, err)
			continue
		}
		if fn != "" && !strings.Contains(string(b), fn) {
			t.Errorf("%s no longer defines %s: update the scope of qwen-review.sh to the file that does", file, strings.TrimSuffix(fn, "("))
		}
	}
	for _, dir := range []string{"internal/auth", "internal/confirm", "internal/config"} {
		if !strings.Contains(s, dir+"/*") {
			t.Errorf("qwen-review.sh: %s/* is not in the scope of the review", dir)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir))); err != nil {
			t.Errorf("%s: %v — the scope names a directory that is gone", dir, err)
		}
	}
}

func qwenScript(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "qwen-review.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
