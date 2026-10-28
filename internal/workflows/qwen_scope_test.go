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

// The review of Qwen comes by itself on every PR but one that changes only
// tests and documents without .github/ and internal/workflows/ — a product
// PR is told by its change (CLAUDE.md §5), the title is one more reason;
// /qwen review runs on any; critical, high and secrets, consent and
// workflows are findings beyond the cap of five (owner's decision #485,
// П1 (б); #372; the release's review of #487). ci tests the choice with git
// and gh faked (qwen_review_scope_test.sh), and the script holds it.
func TestWorkflows_QwenReviewScope(t *testing.T) {
	if !runsStep(readWorkflows(t)["ci.yml"], ".github/scripts/qwen_review_scope_test.sh") {
		t.Error("ci: which PRs Qwen reviews is not tested (qwen_review_scope_test.sh)")
	}
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "qwen-review.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"(продукт"`, ".github/* | internal/workflows/*) review=1", "*_test.go | *.md | docs/*) ;;",
		`"${COMMENT_ID:-}"`, "не больше пяти находок", "«Гипотезы»", "всегда находкой, даже сверх пяти"} {
		if !strings.Contains(s, want) {
			t.Errorf("qwen-review.sh: no %s — the scope or the cap of #485 is gone", want)
		}
	}
}
