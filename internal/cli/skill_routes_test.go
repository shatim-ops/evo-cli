// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// routeToolOutside are the tools a skill names that are not in the groups
// of its route, with why: named to say what not to use, or a group the
// route adds on a condition.
var routeToolOutside = map[string]string{
	"deploy an app or a compose project, update it, its logs: lb_create": "named to say HTTPS is not there: the balancer is L4 TCP, takes no certificate",
	"a VM, open or close a port, a public IP: lb_create":                 "named to say HTTPS is not there: the balancer is L4 TCP, takes no certificate",
}

// Release plan 1.5, coverage of the surfaces (С9, #514): the table of
// evo-scenarios sends the agent to a skill with the tool groups to start
// the server with. Every group of a row is one of --toolsets, and every
// tool the row's skill names (or the row's tools, «tools `kafka_*`») is in
// one of its groups — else the agent, started as the table says, does not
// see the tool the skill tells it to call.
func TestSkills_RouteGroupsHoldTheirTools(t *testing.T) {
	reg := newRegistry()
	group := map[string]string{}
	for _, tl := range reg.Tools() {
		group[tl.Name] = tl.Group
	}
	spans := map[string][]string{}
	var scenarios []string
	for _, s := range readSkills(t) {
		for _, sp := range skillSpans(s.body) {
			spans[s.name] = append(spans[s.name], sp.text)
		}
		if s.name == "evo-scenarios" {
			scenarios = s.body
		}
	}
	span := regexp.MustCompile("`([^`]+)`")
	toolName := regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)+$`)
	rows, used := 0, map[string]bool{}
	var bad []string
	inTable := false
	for _, line := range scenarios {
		// The table of «Which skill»: its header names the groups.
		if strings.Contains(line, "| Tool groups (`--toolsets`) |") {
			inTable = true
			continue
		}
		if !inTable || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			inTable = inTable && strings.HasPrefix(strings.TrimSpace(line), "|")
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 3 || !strings.Contains(cells[2], "`") {
			continue
		}
		var groups []string
		for _, m := range span.FindAllStringSubmatch(cells[2], -1) {
			groups = append(groups, strings.Split(m[1], ",")...)
		}
		if _, err := tools.ParseToolsets(groups); err != nil {
			bad = append(bad, strings.TrimSpace(cells[0])+": "+err.Error())
			continue
		}
		in := map[string]bool{}
		for _, g := range groups {
			in[g] = true
		}
		var named []string
		for _, m := range span.FindAllStringSubmatch(cells[1], -1) {
			switch w := m[1]; {
			case strings.HasPrefix(w, "evo-"):
				for _, n := range spans[w] {
					if _, ok := group[n]; ok && toolName.MatchString(n) {
						named = append(named, n)
					}
				}
			case strings.HasSuffix(w, "_*"):
				for name := range group {
					if strings.HasPrefix(name, strings.TrimSuffix(w, "*")) {
						named = append(named, name)
					}
				}
			}
		}
		rows++
		for _, name := range named {
			key := strings.TrimSpace(cells[0]) + ": " + name
			switch {
			case in["all"] || in[group[name]]:
			case routeToolOutside[key] != "":
				used[key] = true
			default:
				bad = append(bad, key+" is in group "+group[name]+", not in the row's "+strings.Join(groups, ","))
			}
		}
	}
	if rows == 0 {
		t.Fatal("no row of the table of evo-scenarios: the table moved, update the test")
	}
	for key := range routeToolOutside {
		if !used[key] {
			bad = append(bad, key+": in routeToolOutside, but in the row's groups or not named: remove the line")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(dedup(bad), "\n"))
	}
}

func dedup(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}
