// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// reasonKinds are the answers of the cloud TestContract_APIStatusReason
// feeds, and a 2xx whose JSON does not parse: the error a service client
// wraps as «parse <what>: …», which uncodedReason reads by its text
// (parseAnswerRE).
var reasonKinds = []string{"400", "401", "403", "404", "409", "422", "429", "500", "502", "html", "empty", "conn", "badjson"}

// reasonsIn is the reason of every tool on every reasonKinds answer, with
// the CLI's language lang: "tool kind" → reason.
func reasonsIn(t *testing.T, lang string) map[string]string {
	t.Helper()
	i18n.SetLang(lang)
	rt := &contractRT{}
	reg := contractReg(t, rt)
	out := map[string]string{}
	for _, tl := range reg.Tools() {
		if contractHTTPSkip[tl.Name] {
			continue
		}
		args := contractArgs(tl)
		if tl.Mutating {
			args["confirmed"] = true
		}
		for _, kind := range reasonKinds {
			rt.kind, rt.body = kind, ""
			if kind == "badjson" {
				rt.kind, rt.body = "200", `{"items": [`
			}
			limit := contractCallLimit
			if kind == "conn" {
				limit = contractCallLimit / 10
			}
			text, _, pan := contractCall(tl, args, limit)
			rt.take()
			if pan != "" {
				continue
			}
			reason, status := contractReason(text)
			out[tl.Name+" "+kind] = reason + "/" + status
		}
		rt.body = ""
	}
	return out
}

// D6 translates evo's words, never the meaning of an answer: reason and
// with it the exit code are the same in either language of the CLI. The
// reason of an error with no code of its own is read from its text
// (uncodedReason: «not found», «operation … failed», «parse <what>: »,
// «API 500»), so a wrap moved into i18n.T can change it in Russian only —
// «разбор операции: …» no longer matches parseAnswerRE, and an answer the
// cloud got wrong turns from API_ERROR (5) into USAGE (1) (found in review
// of #354, part 2 of the language of the wraps, 1.3.1).
func TestReason_SameInEveryLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	en := reasonsIn(t, i18n.EN)
	ru := reasonsIn(t, i18n.RU)
	if len(en) == 0 {
		t.Fatal("no call answered")
	}
	keys := make([]string, 0, len(en))
	for k := range en {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	diff := 0
	for _, k := range keys {
		if en[k] != ru[k] {
			diff++
			t.Errorf("%s: reason/status %s in English, %s in Russian", k, en[k], ru[k])
		}
	}
	if diff > 0 {
		t.Logf("%d of %d answers differ", diff, len(keys))
	}
}

// The same rule, read from the code: an i18n.T or i18n.Tf whose English
// text a classifier of uncodedReason matches must have a Russian text that
// classifier matches too — or the classifier is taught Russian in the same
// PR. The test above sees the answers its world gives; this one sees every
// text, a failed operation of the cloud too («mk8s operation … failed»).
func TestReason_ClassifiersReadBothLanguages(t *testing.T) {
	classifiers := map[string]func(string) bool{
		"parseAnswerRE":     func(s string) bool { return parseAnswerRE.MatchString(strings.ToLower(s)) },
		"operationFailedRE": func(s string) bool { return operationFailedRE.MatchString(strings.ToLower(s)) },
		"not found": func(s string) bool {
			s = strings.ToLower(s)
			return strings.Contains(s, "not found") || strings.Contains(s, "не найден")
		},
		"apiStatusRE": apiStatusRE.MatchString,
	}
	root := filepath.Join("..", "..")
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "T" && sel.Sel.Name != "Tf") {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "i18n" {
					return true
				}
				en, ok1 := stringLit(call.Args[0])
				ru, ok2 := stringLit(call.Args[1])
				if !ok1 || !ok2 {
					return true
				}
				for name, match := range classifiers {
					// A format verb stands for the words a call puts there.
					e, r := wrapSample(en), wrapSample(ru)
					if match(e) && !match(r) {
						t.Errorf("%s:%d: %s reads %q, not its Russian %q — the reason would differ in the Russian CLI: keep the classified words or teach %s Russian",
							filepath.ToSlash(rel), fset.Position(call.Pos()).Line, name, en, ru, name)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// wrapSample is a format with its verbs filled the way a call fills them.
func wrapSample(format string) string {
	return strings.NewReplacer("%w", "x", "%v", "x", "%s", "x", "%q", `"x"`, "%d", "1").Replace(format)
}
