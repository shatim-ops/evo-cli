// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
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
	// A 2xx without a receipt is an unknown outcome: the intent stays
	// open (D92) — not found by a sign of code, so by hand.
	"internal/receipt/receipt.go": "func Decode(",
	"internal/mcp/http_policy.go": "",
	"internal/mcp/http_kube.go":   "",
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

// policyName is the name of a declaration of the code of secrets and of
// the layer of intents: a secret tool, a filter of a secret, a redaction,
// a reveal in plaintext, the record of a write that may be repeated (D92).
var policyName = regexp.MustCompile(`(?i)secret|redact|sensitive|intent`)

// declaredPolicy names a top-level declaration of f whose name is
// policyName's, and returns the unexported types it declares; "" — none.
func declaredPolicy(f *ast.File) (why string, types []string) {
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if why == "" && policyName.MatchString(x.Name.Name) {
				why = "declares " + x.Name.Name
			}
		case *ast.GenDecl:
			for _, sp := range x.Specs {
				switch y := sp.(type) {
				case *ast.ValueSpec:
					for _, n := range y.Names {
						if why == "" && policyName.MatchString(n.Name) {
							why = "declares " + n.Name
						}
					}
				case *ast.TypeSpec:
					// Only the layer's own types: an exported one
					// (Tool, Handler) is the whole registry's.
					if !ast.IsExported(y.Name.Name) {
						types = append(types, y.Name.Name)
					}
					if why == "" && policyName.MatchString(y.Name.Name) {
						why = "declares " + y.Name.Name
					}
				}
			}
		}
	}
	if why == "" {
		types = nil
	}
	return why, types
}

// policyUse names why f is the code of secrets or of intents by what it
// does: it repeats the user's text in a preview (EchoedArgs, echoedArg),
// it uses the intent records (any intent.X: the record, its
// directory, the refusal that closes it),
// declares secret inputs (SecretFiles: in a literal) or reads them
// (x.SecretFiles), calls a function of the package whose name is
// policyName's (policyCalls: the filter of secret answers called from the
// journal of calls), or declares a function with a type of the layer
// (layerTypes) in its signature — a helper of the layer, like the targets
// and the settling of intents. "" — none.
func policyUse(f *ast.File, layerTypes, policyCalls map[string]bool) string {
	why := ""
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok && id.Name == "SecretFiles" {
				why = "declares secret inputs (SecretFiles)"
			}
		case *ast.Ident:
			// The text of the user a preview repeats (Decl.EchoedArgs):
			// a template with a password reached the preview (#2138).
			if x.Name == "echoedArg" || x.Name == "EchoedArgs" {
				why = "repeats the user's text in a preview (" + x.Name + ")"
			}
		case *ast.SelectorExpr:
			if x.Sel.Name == "SecretFiles" {
				why = "reads secret inputs (SecretFiles)"
			}
			if pkg, ok := x.X.(*ast.Ident); ok && pkg.Name == "intent" {
				why = "uses the intent records (intent." + x.Sel.Name + ")"
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && policyCalls[id.Name] {
				why = "calls " + id.Name
			}
		case *ast.FuncDecl:
			ast.Inspect(x.Type, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok && layerTypes[id.Name] {
					why = "declares " + x.Name.Name + " with " + id.Name + " of the layer"
				}
				return why == ""
			})
		}
		return why == ""
	})
	return why
}

// policyCode names why each non-test file of dir is the code of secrets or
// of intents: the package's name is policyName's; or, in a package that
// holds the policy (internal/tools, internal/mcp), a declaration of such a
// name; or what policyUse finds. Files that are not — not in the map.
func policyCode(t *testing.T, root, dir string, holdsPolicy bool) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*ast.File{}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files[dir+"/"+filepath.Base(p)] = f
	}
	found := map[string]string{}
	layerTypes := map[string]bool{}
	policyCalls := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && policyName.MatchString(fd.Name.Name) {
				policyCalls[fd.Name.Name] = true
			}
		}
	}
	for rel, f := range files {
		if policyName.MatchString(f.Name.Name) {
			found[rel] = "is package " + f.Name.Name
			continue
		}
		if !holdsPolicy {
			continue
		}
		if why, types := declaredPolicy(f); why != "" {
			found[rel] = why
			for _, ty := range types {
				layerTypes[ty] = true
			}
		}
	}
	for rel, f := range files {
		if _, ok := found[rel]; ok {
			continue
		}
		lt, pc := layerTypes, policyCalls
		if !holdsPolicy {
			lt, pc = nil, nil
		}
		if why := policyUse(f, lt, pc); why != "" {
			found[rel] = why
		}
	}
	return found
}

// The code of secrets and of the layer of intents is in the scope of the
// review that comes by itself, found by its code and not by a hand list:
// sensitiveCode did not see secret_writes.go, the filter of the answers of
// secret writes (D105, D114; #2000), and Qwen did not come to #2102; nor
// intent.go, the layer of intents and the consent on a repeat (D92), and it
// did not come to #2115 (plan 1.9, «Область ревью Qwen не видит новых
// файлов секретов»).
func TestWorkflows_QwenScopeFindsPolicyCode(t *testing.T) {
	s := qwenScript(t)
	root := filepath.Join("..", "..")
	var dirs []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			dirs = append(dirs, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, dir := range dirs {
		holds := dir == "internal/tools" || dir == "internal/mcp"
		for rel, why := range policyCode(t, root, dir, holds) {
			n++
			if !strings.Contains(s, rel) && !strings.Contains(s, dir+"/*") {
				t.Errorf("qwen-review.sh: %s %s, but is not in the scope of the review", rel, why)
			}
		}
	}
	if n < 10 {
		t.Errorf("found %d files of secrets and intents: the signs no longer see the code", n)
	}
}

// The signs tell the code of secrets and intents from the rest by code: a
// declaration, secret inputs, the record of an intent, a helper with a type
// of the layer; a word in a comment or a string does not count.
func TestQwenScope_PolicyCodeIsFoundByCode(t *testing.T) {
	cases := map[string]bool{
		"package p\nfunc filterSecretAnswer() {}":                                      true,
		"package p\nvar redactedKeys = 1":                                              true,
		"package p\nvar specs = map[string]S{\"a\": {SecretFiles: map[string]bool{}}}": true,
		"package p\nfunc f(t T) bool { return t.CLI.SecretFiles[\"k\"] }":              true,
		"package p\nfunc f(e []echoedArg) {}":                                          true,
		"package p\nfunc f(in I) { intent.Close(in) }":                                 true,
		"package p\nfunc settled() (outcome, error) { return 0, nil }":                 true,
		"package p\n// secret in a comment\nfunc f() string { return \"intent\" }":     false,
	}
	for src, want := range cases {
		f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		why, _ := declaredPolicy(f)
		if why == "" {
			why = policyUse(f, map[string]bool{"outcome": true}, map[string]bool{"secretWriteAnswer": true})
		}
		if got := why != ""; got != want {
			t.Errorf("%q: found %v, want %v", src, got, want)
		}
	}
}
