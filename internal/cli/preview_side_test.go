// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// sideRecord is a record a call writes besides the resource it makes: a
// rule of a security group, a floating IP and its attach. route matches
// the write ("METHOD host/path"); named reports whether the preview of the
// same call names that record, by its structure — a key with the value the
// write goes to, not a word in a message.
type sideRecord struct {
	what  string
	route *regexp.Regexp
	named func(preview map[string]any, match []string) bool
}

var sideRecords = []sideRecord{
	{
		what:  "a rule of security group",
		route: regexp.MustCompile(`^POST [^ ]*/security-groups/([^/ ]+)/rules`),
		named: func(p map[string]any, m []string) bool {
			sg, _ := previewValue(p, "security_group").(map[string]any)
			return sg != nil && sg["id"] == m[1]
		},
	},
	{
		what:  "a floating IP",
		route: regexp.MustCompile(`^(POST|PUT) [^ ]*/floating-ips`),
		named: func(p map[string]any, _ []string) bool {
			return previewValue(p, "public_ip") == true || previewValue(p, "floating_ip") == true
		},
	},
}

// ownRecords are the tools whose own resource is such a record — the call
// the person asked for, not a record besides it: "tool<TAB>what".
var ownRecords = map[string]bool{
	"open_ports\ta rule of security group": true,
	"fip_attach\ta floating IP":            true,
}

// sideRecordArgs are cases besides parityArgs' that make a side record the
// parity case does not: provision with a port opens a rule.
var sideRecordArgs = map[string][]map[string]any{
	"provision": {{"name": "app", "ports": []any{"8080"}}},
}

// sideRecordMisses are the side records a preview does not name:
// "tool<TAB>what" → why. It only shrinks. Empty: its deadline 1.7 is met,
// and a new line comes with a deadline of its own and its plan line (release
// plan 1.7, P5: the previews of provision, deploy_plan and deploy name the
// group, whether it is the zone's shared one and the source — one helper).
var sideRecordMisses = map[string]string{}

// P5 (agent runs of Grok on 1.6.0, G2-2; release plan 1.7): every record a
// confirmed call writes besides its resource — a rule of a security group
// (the zone's shared group: it opens the port for every VM in it), a
// floating IP — has its field in the preview of the same call, with the
// value the write goes to (the group's id). On the world of
// TestPreview_NamesWhatTheWriteSends: every mutating tool with parityArgs,
// and the cases of sideRecordArgs.
func TestPreview_NamesEverySideRecord(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	reg := newRegistry()
	type run struct {
		tool  string
		args  map[string]any
		setup func(t *testing.T, w parityWorld) (map[string]any, func())
	}
	var runs []run
	seen := map[string]bool{}
	for _, c := range cases {
		if !c.tool.Mutating || seen[c.tool.Name] || c.state != "" {
			continue
		}
		seen[c.tool.Name] = true
		runs = append(runs, run{c.tool.Name, c.args, c.setup})
		for _, extra := range sideRecordArgs[c.tool.Name] {
			runs = append(runs, run{c.tool.Name, extra, c.setup})
		}
	}
	found := map[string]bool{}
	var bad []string
	for _, rc := range runs {
		tl, _ := reg.Lookup(rc.tool)
		call := func(confirmed bool) (string, []string) {
			args := copyArgs(rc.args)
			done := func() {}
			if rc.setup != nil {
				extra, d := rc.setup(t, w)
				for k, v := range extra {
					args[k] = v
				}
				done = d
			}
			if confirmed {
				args["confirmed"] = true
			}
			fake.reset()
			newProcess()
			flagJSON, flagDryRun, flagYes = false, false, false
			text, _ := tl.Handler(context.Background(), args)
			writes := fake.takeWrites()
			done()
			forgetRun(w)
			return text, writes
		}
		previewText, _ := call(false)
		var preview map[string]any
		_ = json.Unmarshal([]byte(previewText), &preview)
		_, writes := call(true)
		for _, wr := range writes {
			for _, sr := range sideRecords {
				m := sr.route.FindStringSubmatch(wr)
				if m == nil {
					continue
				}
				key := rc.tool + "\t" + sr.what
				if ownRecords[key] || sr.named(preview, m) {
					continue
				}
				found[key] = true
				if _, ok := sideRecordMisses[key]; !ok {
					bad = append(bad, strings.Replace(key, "\t", ": ", 1)+" is written ("+wr[:min(len(wr), 120)]+"), and the preview does not name it by its structure")
				}
			}
		}
	}
	for key := range sideRecordMisses {
		if !found[key] {
			bad = append(bad, strings.Replace(key, "\t", ": ", 1)+": in sideRecordMisses, but the preview names it or the call does not write it; take the line out (the list only shrinks)")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// previewValue is the value of key in the preview: at its top or in its
// "preview" object.
func previewValue(p map[string]any, key string) any {
	if v, ok := p[key]; ok {
		return v
	}
	if inner, ok := p["preview"].(map[string]any); ok {
		return inner[key]
	}
	return nil
}
