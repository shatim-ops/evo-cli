// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

var updateDecls = flag.Bool("update-decls", false, "rewrite internal/tools/testdata/decls.json from the declarations")

const declsSnapshotPath = "testdata/decls.json"

// declSnap is what the declaration of one tool says, as text: the
// snapshot an audit reads and a PR that changes a declaration shows in its
// diff.
type declSnap struct {
	Group          string     `json:"group,omitempty"`
	Project        bool       `json:"project,omitempty"`
	RequiredOneOf  [][]string `json:"required_one_of,omitempty"`
	AlwaysListed   string     `json:"always_listed,omitempty"`
	Sensitive      bool       `json:"sensitive,omitempty"`
	SideEffects    string     `json:"side_effects,omitempty"`
	Destructive    bool       `json:"destructive,omitempty"`
	PartialConsent string     `json:"partial_consent,omitempty"`
	Created        string     `json:"created,omitempty"`
	CreateName     string     `json:"create_name,omitempty"`
	Nameless       string     `json:"nameless,omitempty"`
	Delete         string     `json:"delete,omitempty"`
	IDArgs         []string   `json:"id_args,omitempty"`
	RefArgs        []string   `json:"ref_args,omitempty"`
	// The rest of the declaration went into the snapshot after the move
	// («Риск», conclusion 228): what of the answer is untrusted, what the
	// preview echoes, and Tab's lists and exceptions.
	Untrusted    string            `json:"untrusted,omitempty"`
	EchoedArgs   []string          `json:"echoed_args,omitempty"`
	Completes    map[string]string `json:"completes,omitempty"`
	NoCompletion map[string]string `json:"no_completion,omitempty"`
}

// funcName names a function of a declaration; "" for none.
func funcName(f any) string {
	v := reflect.ValueOf(f)
	if !v.IsValid() || v.IsNil() {
		return ""
	}
	return runtime.FuncForPC(v.Pointer()).Name()
}

// set says whether a function of a declaration is there.
func set(f any) bool { v := reflect.ValueOf(f); return v.IsValid() && !v.IsNil() }

func idArgSnap(a idArg) string {
	s := fmt.Sprintf("%s why=%q target=%v ids=%v", a.name, a.why, a.target, set(a.ids))
	if k := a.kind; k != nil {
		s += fmt.Sprintf(" kind={what:%q byName:%v list:%q check:%v project:%v card:%v}",
			k.what, k.byName, k.listTool, set(k.check), set(k.project), set(k.card))
	}
	return s
}

// declsSnapshot is the declaration of every tool of the registry that
// declares anything, by name.
func declsSnapshot() map[string]declSnap {
	out := map[string]declSnap{}
	for _, tl := range New(nil).Tools() {
		s := declSnap{
			Group:         tl.Decl.Group,
			Project:       tl.Decl.Project,
			RequiredOneOf: tl.Decl.RequiredOneOf,
			AlwaysListed:  tl.Decl.AlwaysListed,
			Sensitive:     tl.Decl.Sensitive,
			SideEffects:   tl.Decl.SideEffects,
			Destructive:   tl.Decl.Destructive,
		}
		s.PartialConsent = funcName(tl.Decl.PartialConsent)
		if c := tl.Decl.Created; c.declared() {
			s.Created = fmt.Sprintf("%+v", c)
		}
		if c := tl.Decl.CreateName; len(c.args) > 0 {
			s.CreateName = fmt.Sprintf("%+v", c)
		}
		s.Nameless = tl.Decl.Nameless
		if d := tl.Decl.Delete; d.arg != "" {
			s.Delete = fmt.Sprintf("%+v", d)
		}
		for _, a := range tl.Decl.IDArgs {
			s.IDArgs = append(s.IDArgs, idArgSnap(a))
		}
		for _, r := range tl.Decl.RefArgs {
			s.RefArgs = append(s.RefArgs, fmt.Sprintf("%+v", r))
		}
		if u := tl.Decl.Untrusted; !reflect.DeepEqual(u, untrustedAnswer{}) {
			s.Untrusted = fmt.Sprintf("%+v", u)
		}
		for _, e := range tl.Decl.EchoedArgs {
			a := e.name
			if f := funcName(e.summary); f != "" {
				a += " summary=" + f
			}
			s.EchoedArgs = append(s.EchoedArgs, a)
		}
		for arg, c := range tl.Decl.Completes {
			if s.Completes == nil {
				s.Completes = map[string]string{}
			}
			s.Completes[arg] = fmt.Sprintf("%+v", c)
		}
		s.NoCompletion = tl.Decl.NoCompletion
		if !reflect.DeepEqual(s, declSnap{}) {
			out[tl.Name] = s
		}
	}
	return out
}

// The declarations of the tools are the snapshot taken of the maps they
// were moved from (Tool.Decl, step 0: docs/internal/briefs/modules.md, §5;
// #1755, #1756): an expectation not drawn from decls, so a declaration
// changed or lost in a move — consent taken from a tool, a project check
// dropped, an argument of a one-of set replaced — fails here, and a PR
// that changes one on purpose shows it in the diff of the snapshot.
func TestDecl_Snapshot(t *testing.T) {
	b, err := json.MarshalIndent(declsSnapshot(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if *updateDecls {
		if err := os.WriteFile(filepath.FromSlash(declsSnapshotPath), b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	old, err := os.ReadFile(filepath.FromSlash(declsSnapshotPath))
	// A Windows checkout may hold the file with CRLF (as docs_test does).
	old = bytes.ReplaceAll(old, []byte("\r\n"), []byte("\n"))
	if err != nil || !bytes.Equal(old, b) {
		t.Errorf("internal/tools/testdata/decls.json is not what the declarations say: go test ./internal/tools -run TestDecl_Snapshot -update-decls")
	}
}

// Each line identifies one literal in a decision, including repeated tests
// in the same function and compound completion keys such as "s3_get key".
// Removing a decision requires deleting its line from the baseline.
func TestToolNameComparisonsOnlyShrink(t *testing.T) {
	r := New(nil)
	var found []string
	for _, file := range []string{"intent.go", "intent_targets.go", "completion.go", "history.go", "tools_history.go", "preview_project.go"} {
		tree, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range tree.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			record := func(x ast.Expr) {
				lit, ok := ast.Unparen(x).(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				name, _, _ := strings.Cut(value, " ")
				if _, ok := r.Lookup(name); ok || name == "login" {
					found = append(found, fmt.Sprintf("%s\t%s\t%s", file, fn.Name.Name, value))
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BinaryExpr:
					if x.Op == token.EQL || x.Op == token.NEQ {
						record(x.X)
						record(x.Y)
					}
				case *ast.SwitchStmt:
					if x.Tag != nil {
						for _, stmt := range x.Body.List {
							for _, v := range stmt.(*ast.CaseClause).List {
								record(v)
							}
						}
					}
				case *ast.CallExpr:
					if f, ok := x.Fun.(*ast.SelectorExpr); ok && f.Sel.Name == "EqualFold" {
						for _, v := range x.Args {
							record(v)
						}
					}
				}
				return true
			})
		}
	}
	checkDecisionTable(t, "tool_name_comparisons.tsv", found)
}

// Record the registry's effective repeat rule, consent and canonical preview
// binding, timing of the intent receipt, and choice of target arguments.
func TestRegistryDecisionSnapshot(t *testing.T) {
	r := contractReg(t, &contractRT{kind: "200"})
	var rows []string
	for _, tool := range r.Tools() {
		rule := everySendIsAChange[tool.Name]
		repeat := rule.read
		if rule.made {
			repeat += ":made"
		}
		if rule.notBilled {
			repeat += ":unbilled"
		}
		if rule.settledBy != nil {
			repeat += ":settle"
		}
		consent := "none"
		if takesConsent(tool) {
			consent = "always"
			if !AlwaysNeedsConsent(tool) {
				consent = "conditional"
			}
		}
		props, _ := tool.InputSchema["properties"].(map[string]any)
		if _, ok := props["preview_id"]; ok {
			consent += ":preview-id"
			if bytes.Equal(r.canonArgs(tool.Name, map[string]any{"preview_id": "a"}), r.canonArgs(tool.Name, map[string]any{"preview_id": "b"})) {
				consent += ":outside-canon"
			}
		}
		receipt := sampleReceipt(t, r, tool)
		sample := map[string]any{}
		for key := range props {
			sample[key] = key
		}
		name, target := intentResource(tool.Name, sample)
		var targets []string
		for _, a := range targetArgs(tool.Name) {
			targets = append(targets, a.name)
		}
		if rule.target != nil {
			targets = append(targets, "resolver")
		}
		fields := []string{tool.Name, repeat, consent, receipt, name, target, strings.Join(targets, ",")}
		for i, v := range fields {
			if v == "" {
				fields[i] = "-"
			}
		}
		rows = append(rows, strings.Join(fields, "\t"))
	}
	checkDecisionTable(t, "registry_decisions.tsv", rows)
}

// Observe the real consent gate with an isolated intent directory. The
// simulated first write marks the transport boundary without sending a request.
func sampleReceipt(t *testing.T, r *Registry, tool Tool) string {
	t.Helper()
	dir := t.TempDir()
	r.SetStateDir(dir)
	args := map[string]any{"confirmed": true}
	if tool.Decl.PartialConsent != nil {
		args = contractArgs(tool)
		args["confirmed"] = true
	}
	receipt := ""
	current := func() bool {
		records := intent.Pending(intent.Dir(dir))
		if len(records) > 1 || len(records) == 1 && records[0].Tool != tool.Name {
			t.Fatalf("%s: unexpected receipt in isolated directory", tool.Name)
		}
		return len(records) == 1
	}
	handler := r.intended(tool, func(ctx context.Context, _ map[string]any) (string, bool) {
		receipt = "none"
		if current() {
			receipt = "before-handler"
		}
		if err := httpretry.NoteWrite(ctx); err != nil {
			t.Fatalf("%s: marking the simulated write: %v", tool.Name, err)
		}
		if current() && receipt == "none" {
			receipt = "before-write"
		}
		return `{"status":"ok"}`, false
	})
	text, failed := handler(context.Background(), args)
	if failed || receipt == "" {
		t.Fatalf("%s: receipt control did not reach the handler: %s", tool.Name, text)
	}
	return receipt
}

func checkDecisionTable(t *testing.T, file string, actual []string) {
	t.Helper()
	sort.Strings(actual)
	path := filepath.Join("testdata", file)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ReplaceAll(string(want), "\r\n", "\n")
	expected := strings.Split(strings.TrimSuffix(normalized, "\n"), "\n")
	if slices.Equal(expected, actual) {
		return
	}
	// Sorted multisets preserve duplicate comparisons in one function.
	for len(expected) > 0 || len(actual) > 0 {
		switch {
		case len(expected) > 0 && (len(actual) == 0 || expected[0] < actual[0]):
			t.Errorf("%s: removed: %s", file, expected[0])
			expected = expected[1:]
		case len(actual) > 0 && (len(expected) == 0 || actual[0] < expected[0]):
			t.Errorf("%s: added: %s", file, actual[0])
			actual = actual[1:]
		default:
			expected = expected[1:]
			actual = actual[1:]
		}
	}
}
