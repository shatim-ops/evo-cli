// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

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

// worldDrift are the ways the worlds are known to answer otherwise than the
// platform: "answer id: what" → the work item that makes the world follow
// the platform. The list only shrinks: a drift not here fails the test, and
// so does a line the worlds no longer drift by.
// Closes by 1.8: a line lives at most two versions (release plan 1.6,
// «храповики со сроком»; TestMisses_HaveADeadline).
var worldDrift = map[string]string{
	// CMP-25: the platform sends no «available» of a zone of a flavor; the
	// world goes with the fix of ZoneNames (TestFlavor_ZonesFromTheLiveAnswer).
	"compute-flavors (go previewCloud): items.[].availability_zones.[].available is sent by the world, not by the platform": "release plan 1.6, «Зоны flavor по живому ответу» (CMP-25): «разработка»",
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
			if uuidInPath.MatchString(x) && len(x) == 36 {
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

// Release plan 1.6, «Миры из живых ответов»: ≈50 % of the defects that
// escaped the tests were a wrong model of the platform in the worlds. For
// every line of testdata/platform-answers.json the JSON world answers the
// same request, and its answer keeps to the live form: no path the
// platform does not send (under a path the probe walked), the same kind
// where the probe saw one (a name is no id), and no value of a field of
// state the platform was not seen with — or a line of worldDrift.
func TestPlatformAnswers_WorldsKeepTheLiveForm(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "platform-answers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var answers []platformAnswer
	if err := json.Unmarshal(b, &answers); err != nil {
		t.Fatal(err)
	}
	w, _ := withJSONWorld(t)
	found := map[string]bool{}
	ids := map[string]bool{}
	for _, a := range answers {
		if ids[a.ID] {
			t.Errorf("%s: twice", a.ID)
		}
		ids[a.ID] = true
		if a.Source == "" || a.Recorded == "" || len(a.Paths) == 0 {
			t.Errorf("%s: a line without its source, date or paths", a.ID)
			continue
		}
		if a.Skip != "" {
			continue
		}
		method, url, _ := strings.Cut(a.Request, " ")
		url = strings.NewReplacer("{project}", pvProject, "{id}", parityVM).Replace(url)
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			t.Fatalf("%s: %v", a.ID, err)
		}
		resp, err := w.rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("%s: the world does not answer %s: %v", a.ID, a.Request, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != a.Status {
			t.Errorf("%s: the world answers %d, the platform %d", a.ID, resp.StatusCode, a.Status)
			continue
		}
		kinds, strs, err := answerShape(body)
		if err != nil {
			t.Errorf("%s: the world's answer is not JSON: %v", a.ID, err)
			continue
		}
		checkLiveForm(a, a.ID, kinds, strs, found)
	}
	checkDrift(t, found, "")
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
	}
	for path, allowed := range a.Enums {
		for _, s := range strs[path] {
			if !contains(allowed, s) {
				found[fmt.Sprintf("%s: %s has a value the platform was not seen with", who, path)] = true
			}
		}
	}
}

// checkDrift holds found against worldDrift: a drift not declared fails,
// and so does a line of worldDrift no world drifts by. prefix keeps the
// check to the lines of one test (the HTTP or the Go worlds): "" — the
// lines without " (go " in them, else those with prefix.
func checkDrift(t *testing.T, found map[string]bool, prefix string) {
	t.Helper()
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if worldDrift[k] == "" {
			t.Errorf("%s — make the world answer as the platform does, or, while the plan moves it, a line of worldDrift with the work item", k)
		}
	}
	for k := range worldDrift {
		mine := !strings.Contains(k, " (go ")
		if prefix != "" {
			mine = strings.Contains(k, prefix)
		}
		if mine && !found[k] {
			t.Errorf("worldDrift names %q, which the worlds no longer drift by: remove the line", k)
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

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// goWorlds are the worlds that answer a line of the catalogue through
// provision.CloudAPI rather than over HTTP: answer id → world name → its
// answer, as the body the platform sends (a list under "items").
var goWorlds = map[string]map[string]func() (any, error){
	"compute-flavors": {
		"previewCloud": func() (any, error) { return previewCloud{}.ListFlavors(context.Background()) },
	},
	"compute-floating-ips": {
		"freeFIPCloud": func() (any, error) { return freeFIPCloud{}.ListFloatingIPs(context.Background(), pvProject) },
		"busyFIPCloud": func() (any, error) { return busyFIPCloud{}.ListFloatingIPs(context.Background(), pvProject) },
	},
}

// Release plan 1.6, «Миры из живых ответов», step 2: the worlds that answer
// through provision.CloudAPI keep to the live form too. A world's answer is
// marshalled by the tags evo decodes the API with, its zero values dropped
// (a field the world does not set is not one it sends), and held against
// the line as TestPlatformAnswers_WorldsKeepTheLiveForm holds the HTTP
// worlds. A line skipped for provision.CloudAPI has its worlds here.
func TestPlatformAnswers_GoWorldsKeepTheLiveForm(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "platform-answers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var answers []platformAnswer
	if err := json.Unmarshal(b, &answers); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	seen := map[string]bool{}
	for _, a := range answers {
		worlds, ok := goWorlds[a.ID]
		if strings.Contains(a.Skip, "provision.CloudAPI") != ok {
			t.Errorf("%s: a line skipped for provision.CloudAPI has its worlds in goWorlds, and only such a line", a.ID)
		}
		if !ok {
			continue
		}
		seen[a.ID] = true
		for name, answer := range worlds {
			v, err := answer()
			if err != nil {
				t.Errorf("%s (go %s): %v", a.ID, name, err)
				continue
			}
			raw, err := json.Marshal(map[string]any{"items": v})
			if err != nil {
				t.Fatalf("%s (go %s): %v", a.ID, name, err)
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
			checkLiveForm(a, a.ID+" (go "+name+")", kinds, strs, found)
		}
	}
	for id := range goWorlds {
		if !seen[id] {
			t.Errorf("goWorlds names %s, which no line of the catalogue has", id)
		}
	}
	checkDrift(t, found, " (go ")
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
