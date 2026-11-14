// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
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
