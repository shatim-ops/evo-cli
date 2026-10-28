// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// descriptionWords are the snake_case words of the registry's descriptions
// that are no tool, argument or field evo's code names, with why: the
// platform's own words, which evo passes on as they are.
var descriptionWords = map[string]string{
	"access_ru":                           "part of a group name, SSH-access_ru.AZ-2",
	"admin_password":                      "a field of an image's metadata, filled by --meta",
	"authorized_keys":                     "the file on the VM",
	"connection_string":                   "a field of the PostgreSQL API answer, passed on as is",
	"cpu_usage":                           "a metric of Cloud.ru Monitoring",
	"finops_report":                       "the script billing_finops ports",
	"free_tier_disk_resize_is_prohibited": "an error code of the compute API",
	"invalid_disk_state_to_create_image":  "an error code of the compute API",
	"is_required":                         "a field the image API rejects in image_metadata",
	"kafka_hosts":                         "a field of the Kafka API answer, passed on as is",
	"node_load1":                          "a metric of Cloud.ru Monitoring",
	"redis_hosts":                         "a field of the Redis API answer, passed on as is",
	"too_long":                            "an error code of the compute API",
}

// Release plan 1.5, С9 (audit blind-spots-1.5, FB15-20, 48): what a tool's
// description or an argument's tells the agent to use exists. Every
// snake_case word of them is a tool, an argument of a tool, a field the
// code names (a string literal or a json tag of internal/ and cmd/), or a
// line of descriptionWords with why; a line no description uses fails.
func TestDescriptions_NamesExist(t *testing.T) {
	reg := newRegistry()
	known := map[string]bool{}
	for _, tl := range reg.Tools() {
		known[tl.Name] = true
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for k := range props {
			known[k] = true
		}
	}
	for w := range codeWords(t) {
		known[w] = true
	}
	under := regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)
	used := map[string]bool{}
	var bad []string
	for _, tl := range reg.Tools() {
		texts := []string{tl.Description}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, p := range props {
			if m, ok := p.(map[string]any); ok {
				if d, ok := m["description"].(string); ok {
					texts = append(texts, d)
				}
			}
		}
		for _, text := range texts {
			for _, w := range under.FindAllString(text, -1) {
				switch {
				case known[w]:
				case descriptionWords[w] != "":
					used[w] = true
				default:
					bad = append(bad, tl.Name+": `"+w+"` is no tool, argument or field of evo — a typo, a renamed argument, or a line of descriptionWords with why")
				}
			}
		}
	}
	for w := range descriptionWords {
		if !used[w] {
			bad = append(bad, w+": in descriptionWords, but no description uses it: remove the line")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// С9: a hint of an answer that names a call — "poll kafka_operation(
// operation_id)" — names a tool and an argument of that tool: an argument
// renamed in the schema and not in the hint sends the agent to a call
// that fails.
func TestHints_CallsNameTheToolsArguments(t *testing.T) {
	reg := newRegistry()
	call := regexp.MustCompile(`\b([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\(([a-z][a-z0-9_]*)[=),]`)
	n := 0
	var bad []string
	walkSource(t, func(path string, src []byte) {
		for _, m := range call.FindAllSubmatch(src, -1) {
			tool, arg := string(m[1]), string(m[2])
			tl, ok := reg.Lookup(tool)
			if !ok {
				continue // a Go function call, not a hint
			}
			n++
			props, _ := tl.InputSchema["properties"].(map[string]any)
			if _, ok := props[arg]; !ok {
				bad = append(bad, path+": "+tool+"("+arg+"…): "+arg+" is no argument of "+tool)
			}
		}
	})
	if n == 0 {
		t.Fatal("no hint of the form tool(arg) found: the pattern moved, update the test")
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// codeWords are the identifiers the product code names: whole string
// literals and json tags of internal/ and cmd/ (tests aside).
func codeWords(t *testing.T) map[string]bool {
	t.Helper()
	words := map[string]bool{}
	lit := regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_]*)"`)
	tag := regexp.MustCompile(`json:\\?"([a-z0-9_]+)`)
	walkSource(t, func(_ string, src []byte) {
		for _, re := range []*regexp.Regexp{lit, tag} {
			for _, m := range re.FindAllSubmatch(src, -1) {
				words[string(m[1])] = true
			}
		}
	})
	return words
}

// walkSource calls f with every Go file of internal/ and cmd/ but tests.
func walkSource(t *testing.T, f func(path string, src []byte)) {
	t.Helper()
	for _, dir := range []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			f(filepath.ToSlash(path), b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
