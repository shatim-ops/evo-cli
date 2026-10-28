// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// inventoryFilters are the only keys a skill may put a resource into the
// table to delete by (owner's decision 28.10, release plan 1.4.1, item 11):
// the person's name prefix, and for a VM of a deployment the name deploy
// gives it, evo-<project_name> (vmNameFor), with the prefix.
var inventoryFilters = []string{
	"whose name starts with the prefix",
	"whose `vm_name` starts with `evo-<prefix>`",
}

var (
	skillTool     = regexp.MustCompile("`([a-z][a-z0-9_]*)`")
	inventoryStep = regexp.MustCompile(`^\d+\. Inventory\.`)
	nextStep      = regexp.MustCompile(`^\d+\. `)
)

// deleteTools are the tools a skill names, in SKILL.md and its references,
// that delete: mutating tools named *_delete, *_rm or destroy.
func deleteTools(t *testing.T, s scenarioSkill) map[string]bool {
	t.Helper()
	text := strings.Join(s.body, "\n")
	refs, _ := filepath.Glob(filepath.Join("..", "..", "skills", s.name, "references", "*.md"))
	for _, f := range refs {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text += "\n" + string(b)
	}
	reg := newRegistry()
	out := map[string]bool{}
	for _, m := range skillTool.FindAllStringSubmatch(text, -1) {
		tl, ok := reg.Lookup(m[1])
		if ok && tl.Mutating && (strings.HasSuffix(tl.Name, "_delete") || strings.HasSuffix(tl.Name, "_rm") || strings.HasSuffix(tl.Name, "destroy")) {
			out[tl.Name] = true
		}
	}
	return out
}

// A skill that removes what it finds rather than what it made (it names
// more than one tool that deletes, or it has a step «Inventory») puts into
// its table to delete only what is the person's by a key: a name with the
// prefix, or a VM of a deployment whose vm_name is evo-<prefix>…. Its
// inventory says what it is — «The inventory is … and nothing else» —
// each part of that list naming one of inventoryFilters, and it reads its
// sources only. Escaped (#445, caught by the release's review before the
// merge): an edit of evo-cleanup put into the inventory the VMs of every
// deployment of every working directory, without a filter; only a check of
// the text of one skill stood in the way (TestSkills_SayWhatTheCodeDoes).
func TestSkills_InventoryIsOnlyTheirs(t *testing.T) {
	reg := newRegistry()
	checked := 0
	for _, s := range readSkills(t) {
		var step []string
		for i, l := range s.body {
			if !inventoryStep.MatchString(l) {
				continue
			}
			step = append(step, l)
			for _, m := range s.body[i+1:] {
				if nextStep.MatchString(m) || strings.HasPrefix(m, "#") {
					break
				}
				step = append(step, m)
			}
			break
		}
		deletes := deleteTools(t, s)
		if step == nil {
			if len(deletes) > 1 {
				t.Errorf("skills/%s: deletes with %d tools and has no step «Inventory»: what it may delete is not said", s.name, len(deletes))
			}
			continue
		}
		checked++
		text := strings.Join(strings.Fields(strings.Join(step, " ")), " ")
		sources, rest, ok := strings.Cut(text, "The inventory is ")
		list, _, ok2 := strings.Cut(rest, ", and nothing else")
		if !ok || !ok2 {
			t.Errorf("skills/%s: the step «Inventory» does not say «The inventory is …, and nothing else»", s.name)
			continue
		}
		for _, part := range strings.Split(list, ", and ") {
			named := false
			for _, f := range inventoryFilters {
				named = named || strings.Contains(part, f)
			}
			if !named {
				t.Errorf("skills/%s: the inventory takes %q — by no key of %q: a resource the person did not make would be a row of the table to delete", s.name, part, inventoryFilters)
			}
		}
		// The sources are what the step reads before it says what the
		// inventory is; a tool named after it explains a name.
		for _, m := range skillTool.FindAllStringSubmatch(sources, -1) {
			if tl, ok := reg.Lookup(m[1]); ok && tl.Mutating {
				t.Errorf("skills/%s: the inventory reads %s, which writes: the inventory only reads", s.name, tl.Name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no skill has a step «Inventory»: the test proves nothing")
	}
}
