// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// toolBudget is the bytes one tool may take in tools/list: its entry as
// the server sends it (name, description, inputSchema with the
// descriptions of the arguments, annotations), JSON. tools/list goes to
// every session of every agent; the growth of the whole is the ratchet of
// the pull request (tools-list.yml), this one is the share of each tool.
const toolBudget = 1500

// toolsOverBudget are the tools whose entry is over toolBudget, each with
// its ceiling: the bytes it takes now. A ceiling only goes down — an entry
// above it fails, and so does one below it (lower the ceiling, the bytes
// are not to be spent again). It only shrinks. Closes by 1.8 (release plan
// 1.7: P13, the shortening of the long descriptions inside D68, with a
// budget of bytes per description; what P13 does not reach in 1.7 goes on
// in 1.8).
var toolsOverBudget = map[string]int{
	"deploy_plan": 2750,
	"pg_create":   1669,
	"provision":   2352,
}

// P13 and D68 (release plan 1.7; agent runs of Grok on 1.6.0): D68 moves
// what the scenario skills knew into the registry, P13 shortens the long
// descriptions — without a budget each would undo the other. The budget
// holds every tool's entry of tools/list: a new tool, or a tool that grows
// past toolBudget, fails; a tool over it keeps to its ceiling. Knowledge
// that does not fit goes into the card or the built-in skill (D68's
// invariant of coverage), not into a longer entry.
func TestToolsList_EachToolWithinItsBudget(t *testing.T) {
	var bad []string
	seen := map[string]bool{}
	for _, tl := range New(nil, "test").ListTools() {
		b, err := json.Marshal(tl)
		if err != nil {
			t.Fatal(err)
		}
		n := len(b)
		seen[tl.Name] = true
		ceiling, over := toolsOverBudget[tl.Name]
		switch {
		case !over && n > toolBudget:
			bad = append(bad, tl.Name+": "+strconv.Itoa(n)+" bytes in tools/list, over the budget of "+strconv.Itoa(toolBudget)+" — shorten it, or move what it says into the card or the built-in skill")
		case over && n > ceiling:
			bad = append(bad, tl.Name+": "+strconv.Itoa(n)+" bytes, over its ceiling of "+strconv.Itoa(ceiling)+" (toolsOverBudget) — the ceiling only goes down")
		case over && n <= toolBudget:
			bad = append(bad, tl.Name+": "+strconv.Itoa(n)+" bytes, within the budget: take its line out of toolsOverBudget")
		case over && n < ceiling:
			bad = append(bad, tl.Name+": "+strconv.Itoa(n)+" bytes, under its ceiling of "+strconv.Itoa(ceiling)+": lower the ceiling to "+strconv.Itoa(n))
		}
	}
	for name := range toolsOverBudget {
		if !seen[name] {
			bad = append(bad, name+": in toolsOverBudget, no such tool in tools/list")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}
