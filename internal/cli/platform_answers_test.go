// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
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
var worldDrift = map[string]string{}

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
		for path, kind := range kinds {
			if !livePath(a, path) {
				found[fmt.Sprintf("%s: %s is sent by the world, not by the platform", a.ID, path)] = true
				continue
			}
			if live := a.Paths[path]; live != "" && live != "?" && live != kind {
				found[fmt.Sprintf("%s: %s is %s, live %s", a.ID, path, kind, live)] = true
			}
		}
		for path, allowed := range a.Enums {
			for _, s := range strs[path] {
				if !contains(allowed, s) {
					found[fmt.Sprintf("%s: %s has a value the platform was not seen with", a.ID, path)] = true
				}
			}
		}
	}
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
		if !found[k] {
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
