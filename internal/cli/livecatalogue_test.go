// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

// The check of the worlds against the catalogue of live answers (release
// plan 1.6, «Миры из живых ответов»): one file, the same in internal/cli,
// internal/tools and internal/engine but for its package line
// (TestLiveCatalogue_CopiesSame), as fault_fake_test.go — a test helper
// shared by packages of tests, with no package for it that deadcode would
// not call dead. The catalogue is internal/cli/testdata/platform-answers.json.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// catalogueUUID is a UUID in a value of an answer.
var catalogueUUID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// platformAnswer is a line of testdata/platform-answers.json: the form of a
// live answer of Cloud.ru — the leaf paths of its body, without values —
// with where it was seen (release plan 1.6, «Миры из живых ответов»;
// audit process-test-strategy-1.6 §5, item 1). A path's kind is what a
// probe saw of its value: "name" (a string that is no UUID), "uuid", or
// "?" when the probe saw only the path. enums are the values a field of
// state was seen with; open are the top-level keys the probe did not walk.
type platformAnswer struct {
	ID       string              `json:"id"`
	Request  string              `json:"request"`
	Status   int                 `json:"status"`
	Source   string              `json:"source"`
	Recorded string              `json:"recorded"`
	Paths    map[string]string   `json:"paths"`
	Open     []string            `json:"open"`
	Enums    map[string][]string `json:"enums"`
	// Skip is why the JSON world cannot answer the request over HTTP.
	Skip string `json:"skip"`
}

// answerShape is the form of a JSON body: each leaf path ("a.[].b") with
// the kind of its values — "uuid", "name" (any other string), "number",
// "bool", "null", "object" or "array" for an empty one — and the strings
// seen at each path (for the enums). No value leaves this function but
// through strs, which the caller compares and never prints.
func answerShape(body []byte) (map[string]string, map[string][]string, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, nil, err
	}
	kinds, strs := map[string]string{}, map[string][]string{}
	var walk func(v any, path string)
	walk = func(v any, path string) {
		join := func(k string) string {
			if path == "" {
				return k
			}
			return path + "." + k
		}
		switch x := v.(type) {
		case map[string]any:
			if len(x) == 0 {
				kinds[path] = "object"
			}
			for k, e := range x {
				walk(e, join(k))
			}
		case []any:
			if len(x) == 0 {
				kinds[path] = "array"
			}
			for _, e := range x {
				walk(e, join("[]"))
			}
		case string:
			kind := "name"
			if catalogueUUID.MatchString(x) && len(x) == 36 {
				kind = "uuid"
			}
			kinds[path] = kind
			strs[path] = append(strs[path], x)
		case float64:
			kinds[path] = "number"
		case bool:
			kinds[path] = "bool"
		case nil:
			kinds[path] = "null"
		}
	}
	walk(v, "")
	return kinds, strs, nil
}

func TestAnswerShape_PathsKindsNoValues(t *testing.T) {
	kinds, strs, err := answerShape([]byte(`{"id":"00000000-0000-4000-8000-000000000001","name":"web",
		"zones":[{"names":["a","b"]}],"n":2,"ok":true,"x":null,"empty":[],"o":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"id": "uuid", "name": "name", "zones.[].names.[]": "name", "n": "number",
		"ok": "bool", "x": "null", "empty": "array", "o": "object"}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
	if fmt.Sprint(strs["zones.[].names.[]"]) != "[a b]" {
		t.Errorf("strs = %v", strs)
	}
	for _, k := range kinds {
		if strings.Contains(k, "web") || strings.Contains(k, "0000") {
			t.Errorf("a value in the kinds: %v", kinds)
		}
	}
}

// checkLiveForm puts in found how a world's answer (its kinds and strings,
// answerShape) departs from the live form of a: a path the platform does
// not send, another kind, a value of state the platform was not seen with.
// who names the answer in the lines of found.
func checkLiveForm(a platformAnswer, who string, kinds map[string]string, strs map[string][]string, found map[string]bool) {
	for path, kind := range kinds {
		if !livePath(a, path) {
			found[fmt.Sprintf("%s: %s is sent by the world, not by the platform", who, path)] = true
			continue
		}
		if live := a.Paths[path]; live != "" && live != "?" && live != kind {
			found[fmt.Sprintf("%s: %s is %s, live %s", who, path, kind, live)] = true
		}
		if kind != "object" && kind != "array" && liveObject(a, path) {
			found[fmt.Sprintf("%s: %s is a value, live an object", who, path)] = true
		}
	}
	for path, allowed := range a.Enums {
		for _, s := range strs[path] {
			if !catalogueHas(allowed, s) {
				found[fmt.Sprintf("%s: %s has a value the platform was not seen with", who, path)] = true
			}
		}
	}
}

// livePath reports whether path of a world's answer is one the platform
// sends: a live path, a path under or above one, or under a key the probe
// did not walk (open).
func livePath(a platformAnswer, path string) bool {
	for _, o := range a.Open {
		if path == o || strings.HasPrefix(path, o+".") {
			return true
		}
	}
	for live := range a.Paths {
		if path == live || strings.HasPrefix(path, live+".") || strings.HasPrefix(live, path+".") {
			return true
		}
	}
	return false
}

func catalogueHas(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// loadCatalogue reads the catalogue of live answers, from any package of
// internal/.
func loadCatalogue(t *testing.T) []platformAnswer {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "cli", "testdata", "platform-answers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var answers []platformAnswer
	if err := json.Unmarshal(b, &answers); err != nil {
		t.Fatal(err)
	}
	return answers
}

// goWorldsFound holds the Go worlds of a package against the catalogue:
// answer id → world name → its answer (a list, as under "items" of the
// platform's body). A world's answer is marshalled by the tags evo decodes
// the API with, its zero values dropped — a field the world does not set is
// not one it sends — and held as checkLiveForm holds an HTTP answer.
// The departures go to the result as «<id> (go <world>): …».
func goWorldsFound(t *testing.T, worlds map[string]map[string]func() (any, error)) map[string]bool {
	t.Helper()
	byID := map[string]platformAnswer{}
	for _, a := range loadCatalogue(t) {
		byID[a.ID] = a
	}
	found := map[string]bool{}
	for id, ws := range worlds {
		a, ok := byID[id]
		if !ok {
			t.Errorf("a Go world answers %s, which no line of the catalogue has", id)
			continue
		}
		for name, answer := range ws {
			v, err := answer()
			if err != nil {
				t.Errorf("%s (go %s): %v", id, name, err)
				continue
			}
			raw, err := json.Marshal(map[string]any{"items": v})
			if err != nil {
				t.Fatalf("%s (go %s): %v", id, name, err)
			}
			var tree any
			if err := json.Unmarshal(raw, &tree); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(dropZero(tree))
			kinds, strs, err := answerShape(body)
			if err != nil {
				t.Fatal(err)
			}
			checkLiveForm(a, id+" (go "+name+")", kinds, strs, found)
		}
	}
	return found
}

// holdDrift holds found against drift, the departures of the worlds known
// with the work item that makes them follow the platform: a departure not
// declared fails, and so does a line of drift (one mine says is this
// test's) no world departs by.
func holdDrift(t *testing.T, found map[string]bool, drift map[string]string, mine func(string) bool) {
	t.Helper()
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if drift[k] == "" {
			t.Errorf("%s — make the world answer as the platform does, or, while the plan moves it, a line of the drift with the work item", k)
		}
	}
	for k := range drift {
		if mine(k) && !found[k] {
			t.Errorf("the drift names %q, which the worlds no longer depart by: remove the line", k)
		}
	}
}

// liveObject: the platform sends an object at path (a live path under it).
func liveObject(a platformAnswer, path string) bool {
	for live := range a.Paths {
		if strings.HasPrefix(live, path+".") {
			return true
		}
	}
	return false
}

// dropZero drops from a decoded JSON tree the leaves a Go value has when
// its field is not set: false, 0, "", null, and the objects left empty.
func dropZero(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			e = dropZero(e)
			switch y := e.(type) {
			case nil:
				continue
			case bool:
				if !y {
					continue
				}
			case float64:
				if y == 0 {
					continue
				}
			case string:
				if y == "" {
					continue
				}
			case map[string]any:
				if len(y) == 0 {
					continue
				}
			}
			out[k] = e
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, dropZero(e))
		}
		return out
	}
	return v
}

func TestDropZero_LeavesWhatIsSet(t *testing.T) {
	var tree any
	_ = json.Unmarshal([]byte(`{"a":false,"b":0,"c":"","d":null,"e":{"f":false},"g":true,"h":[{"i":1,"j":""}]}`), &tree)
	got, _ := json.Marshal(dropZero(tree))
	if string(got) != `{"g":true,"h":[{"i":1}]}` {
		t.Errorf("dropZero = %s", got)
	}
}
