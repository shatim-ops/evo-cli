// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// D108 («Риск», conclusion 186): a field of the manifest evo does not know
// was dropped by the decoding — a typo in a section made a stack without
// that resource, a typo in a limiting field left its default. stack_plan
// refuses it before the question and before any request: USAGE, every
// unknown path at once, each with the known fields of its level (У1). The
// case of a name is not made strict (У5): encoding/json takes «Name».
func TestStackPlan_UnknownFieldsRefusedBeforeTheCloud(t *testing.T) {
	home := stackHome(t)
	r := raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, nil)
	plan := func(m string) (map[string]any, string, bool) {
		t.Helper()
		var manifest map[string]any
		if err := json.Unmarshal([]byte(m), &manifest); err != nil {
			t.Fatal(err)
		}
		return callNamed(t, r, "stack_plan", map[string]any{"manifest": manifest})
	}
	m, text, isErr := plan(`{"name":"s","_comment":"a note","postgress":{"name":"p"},` +
		`"vm":{"name":"v","pubic_ip":true,"image":"Ubuntu-24.04","zone":"ru.AZ-1"},"secrets":[{"path":"a","valu":"x"}]}`)
	if !isErr || m["reason"] != "USAGE" {
		t.Fatalf("want USAGE for the unknown fields: %s", text)
	}
	want := []string{"manifest._comment", "manifest.postgress", "manifest.secrets[0].valu", "manifest.vm.pubic_ip"}
	unknown, _ := m["unknown_fields"].([]any)
	var got []string
	for _, u := range unknown {
		f, _ := u.(map[string]any)
		p, _ := f["path"].(string)
		got = append(got, p)
		if known, _ := f["known"].([]any); len(known) == 0 {
			t.Errorf("%s: the known fields of its level are not named: %s", p, text)
		}
		if msg, _ := m["error"].(string); !strings.Contains(msg, p) {
			t.Errorf("%s is not in the error: %s", p, text)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("unknown paths %v, want %v (all at once, in order): %s", got, want, text)
	}
	vm := unknownAt(unknown, "manifest.vm.pubic_ip")
	if known, _ := vm["known"].([]any); !slices.Contains(known, any("private")) || !slices.Contains(known, any("zone")) {
		t.Errorf("manifest.vm.pubic_ip: want the fields of vm named: %v", vm)
	}
	if near := unknownAt(unknown, "manifest.postgress")["nearest"]; near != "postgres" {
		t.Errorf("manifest.postgress: want the nearest name postgres, got %v", near)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".evo", "stack-jobs")); len(entries) != 0 {
		t.Errorf("a refused plan saved a job: %d", len(entries))
	}
	// The case of a name, as encoding/json reads it, is taken (У5).
	if m, text, isErr := plan(`{"Name":"s","VPC":{"Name":"v"}}`); isErr || m["status"] != "planned" {
		t.Errorf("names in another case: want the plan: %s", text)
	}
}

func unknownAt(unknown []any, path string) map[string]any {
	for _, u := range unknown {
		if f, _ := u.(map[string]any); f["path"] == path {
			return f
		}
	}
	return nil
}

// A part of the manifest free by its meaning — a map whose keys the
// caller names (labels, variables) — is declared free with its reason at
// the one point of the rule (У4); every other part is closed. A map added
// to the type without a declaration fails here, and a declaration of what
// the type does not have.
func TestStackPlan_FreePartsOfTheManifestAreDeclared(t *testing.T) {
	var maps []string
	var walk func(rt reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		switch rt.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(rt.Elem(), path)
		case reflect.Map:
			maps = append(maps, path)
		case reflect.Struct:
			for _, f := range manifestFields(rt) {
				walk(f.typ, path+"."+f.name)
			}
		}
	}
	walk(reflect.TypeOf(stack.Manifest{}), "manifest")
	for _, p := range maps {
		if manifestFree[p] == "" {
			t.Errorf("%s is a map of the manifest and not declared in manifestFree with its reason", p)
		}
	}
	for p := range manifestFree {
		if !slices.Contains(maps, p) {
			t.Errorf("%s is declared free, but the manifest has no map there", p)
		}
	}
}

// The manifest the description of stack_plan shows the model is a form
// the strict reading takes (У6): every field it names is a field of the
// type, at its level.
func TestStackPlan_TheDescriptionNamesOnlyKnownFields(t *testing.T) {
	tl, _ := New(nil).Lookup("stack_plan")
	i := strings.Index(tl.Description, "manifest = {")
	if i < 0 {
		t.Fatalf("the description shows no manifest: %s", tl.Description)
	}
	form := tl.Description[i+len("manifest = "):]
	// Drop what is said in brackets: defaults, not fields.
	form = regexp.MustCompile(`\([^)]*\)`).ReplaceAllString(form, "")
	var check func(rt reflect.Type, body, path string)
	check = func(rt reflect.Type, body, path string) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		known := map[string]reflect.Type{}
		for _, f := range manifestFields(rt) {
			known[f.name] = f.typ
		}
		depth, start := 0, 0
		for j := 0; j <= len(body); j++ {
			if j < len(body) && (body[j] == '{' || body[j] == '[') {
				depth++
				continue
			}
			if j < len(body) && (body[j] == '}' || body[j] == ']') {
				depth--
				continue
			}
			if j < len(body) && (body[j] != ',' || depth > 0) {
				continue
			}
			part := strings.TrimSpace(body[start:j])
			start = j + 1
			name := strings.TrimSuffix(strings.FieldsFunc(part, func(c rune) bool { return c == '{' || c == '[' })[0], "?")
			ft, ok := known[name]
			if !ok {
				t.Errorf("the description names %s.%s, which the manifest does not have", path, name)
				continue
			}
			if k := strings.IndexAny(part, "{["); k >= 0 {
				inner := strings.Trim(part[k:], "[]")
				check(ft, strings.TrimSuffix(strings.TrimPrefix(inner, "{"), "}"), path+"."+name)
			}
		}
	}
	end := matchingBrace(form)
	check(reflect.TypeOf(stack.Manifest{}), form[1:end], "manifest")
}

// matchingBrace is the index of the brace that closes s[0].
func matchingBrace(s string) int {
	depth := 0
	for i, c := range s {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(s) - 1
}

// A job planned by an earlier version, whose manifest has a field this one
// does not know, is read, reported and torn down as before (У3): the strict
// reading is the input of stack_plan only.
func TestStackJob_OfAnEarlierFormatIsStillRead(t *testing.T) {
	home := stackHome(t)
	j := planJob(t, `{"name":"old","vpc":{"name":"v"}}`)
	file := filepath.Join(home, ".evo", "stack-jobs", j.ID+".json")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	man, _ := raw["manifest"].(map[string]any)
	man["retired_field"] = true
	vpc, _ := man["vpc"].(map[string]any)
	vpc["old_option"] = "x"
	b, _ = json.Marshal(raw)
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(nil)
	m, text, isErr := callNamed(t, r, "stack_status", map[string]any{"job_id": j.ID})
	if isErr || m["status"] != "planned" {
		t.Errorf("a job of an earlier format: want it read: %s", text)
	}
}
