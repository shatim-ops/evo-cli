// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// toolsCallRU is a tool named the CLI way in a Russian text: 'evo tools call
// snat_create' for a tool without a command of its own.
var toolsCallRU = regexp.MustCompile(`evo tools (?:call|describe) ([a-z0-9_]+)`)

// Every tool and every argument of its schema is described in English (for
// the agent: MCP, 'evo tools describe' with EVO_LANG=en) and in Russian (for
// the CLI), both non-empty (docs/internal/cli-mcp-parity/spec.md, REQ-5). A
// Russian text is Russian (it has Cyrillic letters, so an English one copied
// over fails) and speaks of the CLI: no confirmed (the CLI asks or takes
// --yes), no tool names except as 'evo tools call <tool>' for a tool without
// a command (another tool is named by its command). The summary is one
// phrase without a final period. No Russian text describes a tool or an
// argument that does not exist.
func TestRegistry_DescriptionsBothLanguages(t *testing.T) {
	reg := New(func(map[string]any) (*engine.Engine, error) { return nil, nil })
	names := map[string]Tool{}
	for _, tl := range reg.Tools() {
		names[tl.Name] = tl
	}
	checkRU := func(where, s string) {
		t.Helper()
		switch {
		case strings.TrimSpace(s) == "":
			t.Errorf("%s: no Russian text", where)
			return
		case strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) < 0:
			t.Errorf("%s: the Russian text is not Russian: %q", where, s)
		case strings.Contains(s, "confirmed"):
			t.Errorf("%s: the Russian text mentions confirmed (the CLI asks or takes --yes): %q", where, s)
		}
		for _, m := range toolsCallRU.FindAllStringSubmatch(s, -1) {
			named, ok := names[m[1]]
			switch {
			case !ok:
				t.Errorf("%s: 'evo tools call %s' names no tool: %q", where, m[1], s)
			case named.CLI.Verb != "":
				t.Errorf("%s: names %s as 'evo tools call', but it has a command: evo %s %s", where, m[1], named.CLI.Group, named.CLI.Verb)
			}
		}
		rest := toolsCallRU.ReplaceAllString(s, "evo tools call")
		for _, w := range strings.FieldsFunc(rest, func(r rune) bool { return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			if _, ok := names[w]; ok && strings.Contains(w, "_") {
				t.Errorf("%s names the tool %s instead of its command (or 'evo tools call %s'): %q", where, w, w, s)
			}
		}
	}
	var args int
	for _, tl := range reg.Tools() {
		if strings.TrimSpace(tl.Description) == "" {
			t.Errorf("%s: no English description", tl.Name)
		}
		checkRU(tl.Name+" (ShortRU)", tl.ShortRU)
		checkRU(tl.Name+" (DescriptionRU)", tl.DescriptionRU)
		if strings.HasSuffix(tl.ShortRU, ".") {
			t.Errorf("%s: the Russian summary ends with a period: %q", tl.Name, tl.ShortRU)
		}
		props := schemaProps(tl)
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args++
			p, _ := props[k].(map[string]any)
			if d, _ := p["description"].(string); strings.TrimSpace(d) == "" {
				t.Errorf("%s.%s: no English description", tl.Name, k)
			}
			checkRU(tl.Name+"."+k, tl.ArgDescriptionsRU[k])
		}
		for k := range tl.ArgDescriptionsRU {
			if _, ok := props[k]; !ok {
				t.Errorf("%s.%s: a Russian text for an argument the tool does not have", tl.Name, k)
			}
		}
		for k := range toolsRU[tl.Name].Args {
			if _, ok := props[k]; !ok {
				t.Errorf("toolsRU %s.%s: the tool has no such argument", tl.Name, k)
			}
		}
	}
	for name := range toolsRU {
		if _, ok := names[name]; !ok {
			t.Errorf("toolsRU: %s is no tool", name)
		}
	}
	t.Logf("tools=%d arguments=%d", len(names), args)
	if len(names) < 150 || args < 400 {
		t.Fatalf("registry too small: tools=%d arguments=%d", len(names), args)
	}
}

// The Russian texts live beside the schema, never in it: MCP clients get
// InputSchema as it is, and a field more in every property would grow
// tools/list for every session (spec.md, REQ-5).
func TestRegistry_RussianNotInSchema(t *testing.T) {
	reg := New(func(map[string]any) (*engine.Engine, error) { return nil, nil })
	for _, tl := range reg.Tools() {
		for k, raw := range schemaProps(tl) {
			p, _ := raw.(map[string]any)
			for field := range p {
				if strings.HasSuffix(field, "_ru") || strings.HasSuffix(field, "-ru") {
					t.Errorf("%s.%s: Russian field %s in the schema", tl.Name, k, field)
				}
			}
		}
	}
}
