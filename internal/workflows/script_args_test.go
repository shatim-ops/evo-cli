// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// argInt is a number argument of a tool call in a CI script: `--arg
// key=N` of evo tools call, or `"key": N` / `key: N` of a JSON or jq object.
var argInt = regexp.MustCompile(`(?:--arg\s+([a-z_]+)=|"?([a-z_]+)"?\s*:\s*)(\d+)\b`)

// toolWord is a word that may name a registered tool, and the dynamic
// `${kind}_operation` of cloud-live.sh's opwait.
var toolWord = regexp.MustCompile(`\$\{[a-z]+\}_operation|[a-z][a-z0-9]*(?:_[a-z0-9]+)+|\b[a-z]+\b`)

// TestScripts_ArgsWithinSchema: a number argument a cloud-e2e or api-probe
// script passes to a tool is within the maximum of that argument in the
// tool's schema. The scripts run only in the live cloud, so a value over
// the maximum surfaced as USAGE in cloud-e2e rc.1 of 1.7 (run 41814931228:
// pg_operation and *_operation wait_seconds 60 and 120 against 45,
// stack_status 240 against 20, after D69) instead of in CI.
//
// The tool is the one named on the call's line (backslash continuations
// joined); `${kind}_operation` stands for every *_operation tool. A number
// with no tool on its line is held to the smallest maximum of that argument
// among all tools.
func TestScripts_ArgsWithinSchema(t *testing.T) {
	reg := tools.New(nil)
	// bound[tool][arg]; anyMax[arg] — the smallest over tools.
	bound := map[string]map[string]float64{}
	anyMax := map[string]float64{}
	var ops []string
	for _, tl := range reg.Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for k, v := range props {
			p, _ := v.(map[string]any)
			m, ok := num(p["maximum"])
			if !ok {
				continue
			}
			if bound[tl.Name] == nil {
				bound[tl.Name] = map[string]float64{}
			}
			bound[tl.Name][k] = m
			if old, seen := anyMax[k]; !seen || m < old {
				anyMax[k] = m
			}
		}
		if strings.HasSuffix(tl.Name, "_operation") {
			ops = append(ops, tl.Name)
		}
	}
	if _, ok := anyMax["wait_seconds"]; !ok {
		t.Fatal("no tool declares a maximum of wait_seconds: the check sees nothing")
	}

	var files []string
	for _, g := range []string{".github/scripts/*.sh", ".github/workflows/*.yml"} {
		m, err := filepath.Glob(filepath.Join("..", "..", g))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.sh") {
			continue // stubs of a fake evo, not calls of the real one
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range joinedLines(string(raw)) {
			if !strings.Contains(line, "tools call") && !strings.Contains(line, "--arg ") &&
				!strings.Contains(line, "mcp_call") && !strings.Contains(line, "stdio ") && !strings.Contains(line, "hcall ") {
				continue
			}
			named := toolsOnLine(line, bound, ops)
			for _, m := range argInt.FindAllStringSubmatch(line, -1) {
				key := m[1] + m[2]
				v, _ := strconv.ParseFloat(m[3], 64)
				limit, ok := anyMax[key]
				if !ok {
					continue // no tool bounds this argument
				}
				holder, found := "every tool", false
				for _, tn := range named {
					lm, ok := bound[tn][key]
					if ok && (!found || lm < limit) {
						limit, holder, found = lm, tn, true
					}
				}
				checked++
				if v > limit {
					t.Errorf("%s:%d: %s=%v is over the maximum %v of %s (the call answers USAGE): %s",
						filepath.Base(f), n, key, v, limit, holder, strings.TrimSpace(line))
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no bounded number argument found in the scripts: the scan is broken")
	}
}

// joinedLines maps the first line number of each logical line (backslash
// continuations joined) to its text.
func joinedLines(src string) map[int]string {
	out := map[int]string{}
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		start, cur := i+1, lines[i]
		for strings.HasSuffix(strings.TrimRight(cur, " \t"), `\`) && i+1 < len(lines) {
			i++
			cur = strings.TrimSuffix(strings.TrimRight(cur, " \t"), `\`) + " " + lines[i]
		}
		out[start] = cur
	}
	return out
}

// toolsOnLine: the registered tools a line names that bound some argument.
func toolsOnLine(line string, bound map[string]map[string]float64, ops []string) []string {
	var out []string
	for _, w := range toolWord.FindAllString(line, -1) {
		if strings.HasPrefix(w, "${") {
			out = append(out, ops...)
			continue
		}
		if _, ok := bound[w]; ok {
			out = append(out, w)
		}
	}
	return out
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
