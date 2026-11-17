// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The review of Qwen comes by itself only on a PR that touches the rules of
// the workflows (.github/, internal/workflows/) or the code of secrets and
// consent; /qwen review runs on any; critical, high and secrets, consent and
// workflows are findings beyond the cap of five (owner's decision 29.10,
// audit #578, item 9; before — #485, П1 (б)); on a push to a PR it comes
// when the push changes a sensitive path of the PR's diff — a merge of the
// base does not count (owner's decision 03.11, refining #578). ci tests the
// choice with git and gh faked (qwen_review_scope_test.sh), and the script
// holds it.
func TestWorkflows_QwenReviewScope(t *testing.T) {
	if !runsStep(readWorkflows(t)["ci.yml"], ".github/scripts/qwen_review_scope_test.sh") {
		t.Error("ci: which PRs Qwen reviews is not tested (qwen_review_scope_test.sh)")
	}
	s := qwenScript(t)
	for _, want := range []string{".github/* | internal/workflows/*) found=1", "*_test.go) ;;",
		`comm -12 <(sort -u <<<"$changed") <(sort -u <<<"$pushed")`,
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
	"internal/tools/gate.go":          "func (r *Registry) gated(",
	"internal/tools/registry.go":      "func NeedsConsent(",
	"internal/tools/decl.go":          "Sensitive bool",
	"internal/journal/journal.go":     "func Write(",
	"internal/mcp/http.go":            "func (h *HTTPHandler) authorized(",
	"internal/tools/untrusted.go":     "func markUntrusted(",
	"internal/mcp/http_redact.go":     "func redactSecrets(",
	"internal/tools/tools_secret.go":  "",
	"internal/tools/secret_writes.go": "func secretWriteAnswer(",
	"internal/mcp/http_policy.go":     "",
	"internal/mcp/http_kube.go":       "",
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

// secretName is the name of a declaration of the code of secrets: a secret
// tool, a filter of a secret, a redaction, a reveal in plaintext.
var secretName = regexp.MustCompile(`(?i)secret|redact|sensitive`)

// secretCode names why a file is the code of secrets, by its code and not
// its name: it declares a top-level secret*, *redact*, *sensitive* name; it
// declares secret inputs (SecretFiles: in a literal); or it reads them
// (x.SecretFiles). Empty — it is not.
func secretCode(f *ast.File) string {
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if secretName.MatchString(x.Name.Name) {
				return "declares " + x.Name.Name
			}
		case *ast.GenDecl:
			for _, sp := range x.Specs {
				switch y := sp.(type) {
				case *ast.ValueSpec:
					for _, n := range y.Names {
						if secretName.MatchString(n.Name) {
							return "declares " + n.Name
						}
					}
				case *ast.TypeSpec:
					if secretName.MatchString(y.Name.Name) {
						return "declares " + y.Name.Name
					}
				}
			}
		}
	}
	why := ""
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok && id.Name == "SecretFiles" {
				why = "declares secret inputs (SecretFiles)"
			}
		case *ast.SelectorExpr:
			if x.Sel.Name == "SecretFiles" {
				why = "reads secret inputs (SecretFiles)"
			}
		}
		return why == ""
	})
	return why
}

// A file of internal/tools or internal/mcp that declares or filters
// secrets is in the scope of the review that comes by itself, found by its
// code: the hand list of sensitiveCode did not see secret_writes.go, the
// filter of the answers of secret writes (D105, D114; #2000), and Qwen did
// not come to #2102 (plan 1.9, «Область ревью Qwen не видит новых файлов
// секретов»).
func TestWorkflows_QwenScopeFindsSecretCode(t *testing.T) {
	s := qwenScript(t)
	root := filepath.Join("..", "..")
	for _, dir := range []string{"internal/tools", "internal/mcp"} {
		files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no Go files (%v)", dir, err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			rel := dir + "/" + filepath.Base(path)
			if why := secretCode(f); why != "" && !strings.Contains(s, rel) {
				t.Errorf("qwen-review.sh: %s %s, but is not in the scope of the review", rel, why)
			}
		}
	}
}

// secretCode tells the code of secrets from the rest: by a declaration, by
// declaring and by reading secret inputs; a word in a comment or a string
// does not count.
func TestQwenScope_SecretCodeIsFoundByCode(t *testing.T) {
	cases := map[string]bool{
		"package p\nfunc filterSecretAnswer() {}":                                      true,
		"package p\nvar redactedKeys = 1":                                              true,
		"package p\nvar specs = map[string]S{\"a\": {SecretFiles: map[string]bool{}}}": true,
		"package p\nfunc f(t T) bool { return t.CLI.SecretFiles[\"k\"] }":              true,
		"package p\n// secret in a comment\nfunc f() string { return \"secret\" }":     false,
	}
	for src, want := range cases {
		f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		if got := secretCode(f) != ""; got != want {
			t.Errorf("secretCode(%q) = %v, want %v", src, got, want)
		}
	}
}
