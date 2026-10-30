// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// bareRefs are the references known to reach the human output as a bare
// UUID: "tool field" (the key of the JSON answer the UUID is, or "?"; the
// project of the world is always project_id) → the work item of the release
// plan that names them; "* field" is the field in any tool. The list only
// shrinks: a bare UUID not here fails the test, and so does a line no
// output carries.
var bareRefs = map[string]string{
	"* project_id":                              "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"billing_consumption agreement_id":          "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"billing_finops agreement_id":               "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"deploy_apply subnet_id":                    "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"fip_attach interface_id":                   "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"k8s_add_nodepool flavor_id":                "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"k8s_add_nodepool params.nodes_subnet_id":   "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"k8s_add_nodepool params.security_group_id": "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"k8s_options zone":                          "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"k8s_plan flavor_id":                        "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"provision flavor":                          "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"provision region":                          "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"stack_apply vpc":                           "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
	"tg_create interface_id":                    "release plan 1.6, «Линия CLI»: имя ссылки рядом с id, человеку — «имя (id)»: «разработка»",
}

// notRefs are the UUIDs of the human output that are no reference: "tool
// field" or "* field" → why. The id of what a create made is its own id
// (the answer of the call names it, as the "id" of an object would); an
// operation has no name.
var notRefs = map[string]string{
	"* operation_id":                 "the id of the operation the call started: an operation has no name",
	"ar_create registry_id":          "own id: what ar_create made",
	"deploy_apply floating_ip_id":    "own id: the floating IP deploy_apply made",
	"deploy_apply vm_id":             "own id: the VM deploy_apply made",
	"dns_zone_create zone_id":        "own id: what dns_zone_create made",
	"k8s_add_addon cluster_addon_id": "own id: the addon k8s_add_addon installed",
	"k8s_add_nodepool nodepool_id":   "own id: what k8s_add_nodepool made",
	"kafka_create cluster_id":        "own id: what kafka_create made",
	"pg_create cluster_id":           "own id: what pg_create made",
	"provision message":              "own id: the VM provision made, in its message",
	"provision vm_id":                "own id: what provision made",
	"redis_create cluster_id":        "own id: what redis_create made",
	"status vm_id":                   "own id: the VM of the deployment status describes",
	"vpc_create vpc_id":              "own id: what vpc_create made",
}

// Release plan 1.6, «Инвариант «в человеческом выводе нет голого UUID
// ссылки»» (audits/cli-ids-1.6.md §7 п. 1): a person reads «subnet:
// 3f2a…» and cannot tell which subnet. Every command of the registry, in the
// parity world, in its human form (a read; a write: its preview with
// --dry-run and its result with --yes), at COLUMNS=200: every UUID on a line
// is the own id of an object of the answer (a key "id"), an id the call was
// given, part of a command to copy («evo …»), or has the name of its thing
// on the same line (the name the JSON answer gives it: "name" beside "id",
// "<x>_name" beside "<x>_id").
func TestHuman_NoBareReferenceUUID(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	t.Setenv("COLUMNS", "200")
	type call struct {
		c     jsonCase
		mode  []string
		label string
	}
	var calls []call
	done := map[string]bool{}
	for _, c := range cases {
		if c.state != "" {
			continue
		}
		modes := [][]string{nil}
		if c.tool.Mutating {
			modes = [][]string{{"--dry-run"}, {"--yes"}}
		}
		for _, mode := range modes {
			label := strings.TrimSpace(c.tool.Name + " " + strings.Join(mode, ""))
			if !done[label] {
				done[label] = true
				calls = append(calls, call{c, mode, label})
			}
		}
	}
	// run calls c and gives stdout, stdout with stderr, and the strings the
	// call was given (its arguments, its command line).
	run := func(cl call, extra ...string) (string, string, map[string]bool) {
		c := cl.c
		args := copyArgs(c.args)
		end := func() {}
		if c.setup != nil {
			e, d := c.setup(t, w)
			for k, v := range e {
				args[k] = v
			}
			end = d
		}
		line := c.line
		if line == nil {
			cmd := commandsByTool()[c.tool.Name]
			line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
		}
		fake.reset()
		newProcess()
		_, out, errOut := runCLI(t, c.stdin, append(append(append([]string{}, cl.mode...), extra...), line...)...)
		end()
		_ = os.Remove(state.DefaultPath(w.dir))
		given := map[string]bool{}
		for _, v := range args {
			if s, ok := v.(string); ok {
				given[s] = true
			}
		}
		for _, a := range line {
			given[a] = true
		}
		return humanVolatile(out), humanVolatile(out + "\n" + errOut), given
	}
	// The names of the world: every name the JSON answers give an id.
	answers := make([]any, len(calls))
	names := map[string][]string{}
	for i, cl := range calls {
		out, _, _ := run(cl, "--json")
		_ = json.Unmarshal([]byte(out), &answers[i])
		_, n, _ := refIndex(answers[i])
		for id, name := range n {
			names[id] = append(names[id], name)
		}
	}
	found := map[string]string{}
	for i, cl := range calls {
		_, human, given := run(cl)
		own, _, field := refIndex(answers[i])
		for _, l := range strings.Split(human, "\n") {
			if strings.Contains(l, "evo ") {
				continue
			}
		ids:
			for _, id := range uuidInPath.FindAllString(l, -1) {
				if own[id] || given[id] {
					continue
				}
				for _, n := range names[id] {
					if containsWord(l, n) {
						continue ids
					}
				}
				f := lineField(l, field[id])
				switch {
				case id == pvProject:
					f = "project_id"
				case f == "":
					f = "?"
				}
				k := cl.c.tool.Name + " " + f
				if notRefs[k] != "" || notRefs["* "+f] != "" {
					continue
				}
				if _, ok := found[k]; !ok {
					found[k] = cl.label + ": " + clipLine(strings.TrimSpace(l))
				}
			}
		}
	}
	if len(calls) == 0 {
		t.Fatal("no command ran: the cases moved")
	}
	ks := keys(found)
	sort.Strings(ks)
	var bad []string
	any := map[string]bool{}
	for _, k := range ks {
		if f := "* " + k[strings.Index(k, " ")+1:]; bareRefs[f] != "" {
			any[f] = true
			continue
		}
		if bareRefs[k] == "" {
			bad = append(bad, k+": a bare UUID in the human output — "+found[k])
		}
	}
	for k := range bareRefs {
		if found[k] == "" && !any[k] {
			bad = append(bad, "bareRefs names "+k+", which no human output carries bare: remove the line")
		}
		if notRefs[k] != "" {
			bad = append(bad, k+": in bareRefs and notRefs")
		}
	}
	sort.Strings(bad)
	t.Logf("calls: %d; bare references: %d", len(calls), len(found))
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n") + "\n— name the thing on the line («имя (id)»), or, while the plan moves it, add the reference to bareRefs with the work item")
	}
}

// refIndex reads the JSON answer of a call: the ids of its objects (a key
// "id"), the name each id has in the answer ("name" beside "id", "<x>_name"
// beside "<x>_id") and the key each UUID is the value of.
func refIndex(answer any) (own map[string]bool, names map[string]string, field map[string][]string) {
	own, names, field = map[string]bool{}, map[string]string{}, map[string][]string{}
	var walk func(key string, v any)
	walk = func(key string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				s, ok := e.(string)
				if !ok {
					walk(k, e)
					continue
				}
				f := k
				if p, ok := x["key"].(string); ok && k == "value" {
					// A parameter of a preview: {"key": "subnet", "value": …}.
					f = "params." + p
				}
				for _, id := range uuidInPath.FindAllString(s, -1) {
					field[id] = append(field[id], f)
				}
				if !uuidInPath.MatchString(s) {
					continue
				}
				name := ""
				switch {
				case k == "id":
					own[s] = true
					name, _ = x["name"].(string)
				case strings.HasSuffix(k, "_id"):
					name, _ = x[strings.TrimSuffix(k, "_id")+"_name"].(string)
					if _, hasID := x["id"]; name == "" && !hasID {
						// A row of a catalogue keyed by the id: {"name":
						// "gen-2-4", "flavor_id": …}.
						name, _ = x["name"].(string)
					}
				}
				if name != "" && names[s] == "" {
					names[s] = name
				}
			}
		case []any:
			for _, e := range x {
				if s, ok := e.(string); ok {
					for _, id := range uuidInPath.FindAllString(s, -1) {
						field[id] = append(field[id], key+"[]")
					}
					continue
				}
				walk(key, e)
			}
		}
	}
	walk("", answer)
	return own, names, field
}

// lineField is the key of the answer a UUID on line l is the value of: of
// the keys that carry it (one value may be the id of several things in the
// world), the one the line is labelled with («cluster_id  …»), else the
// first by name.
func lineField(l string, keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	keys = slices.Sorted(slices.Values(keys))
	label := strings.TrimSuffix(strings.Fields(l + " .")[0], ":")
	for _, k := range keys {
		if k == label {
			return k
		}
	}
	return keys[0]
}

// containsWord: l has name n as a word of its own, not inside another name
// (the zone ru.AZ-1 is not named by the subnet Default_ru.AZ-1).
func containsWord(l, n string) bool {
	word := func(r byte) bool {
		return r == '_' || r == '-' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
	}
	for i := 0; ; {
		j := strings.Index(l[i:], n)
		if j < 0 || n == "" {
			return false
		}
		j += i
		end := j + len(n)
		if (j == 0 || !word(l[j-1])) && (end == len(l) || !word(l[end])) {
			return true
		}
		i = j + 1
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestHumanRefs_WordAndField(t *testing.T) {
	for _, c := range []struct {
		l, n string
		want bool
	}{
		{"ru.AZ-1 (00000000-0000-0000-0000-0000000000b2)", "ru.AZ-1", true},
		{"subnet-1  Default_ru.AZ-1  00000000-0000-0000-0000-0000000000b2", "ru.AZ-1", false},
		{"web (x)", "web", true},
		{"webapp", "web", false},
		{"anything", "", false},
	} {
		if got := containsWord(c.l, c.n); got != c.want {
			t.Errorf("containsWord(%q, %q) = %v", c.l, c.n, got)
		}
	}
	if f := lineField("cluster_id    00000000-0000-4000-8000-000000000001", []string{"vpc_id", "cluster_id"}); f != "cluster_id" {
		t.Errorf("lineField by label: %q", f)
	}
	if f := lineField("subnet:  x", []string{"subnet_id", "params.subnet_id"}); f != "params.subnet_id" {
		t.Errorf("lineField without a label: %q, want the first by name", f)
	}
}
