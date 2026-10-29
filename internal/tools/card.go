// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"slices"
	"strings"
)

// GroupCard is the card of a tool group for an agent (#515, option A,
// layer B): every tool of the group with its command, what it does, where
// its ids come from, what a create makes and what deletes it, and what to
// wait with — all from the registry's declarations (createdIDs,
// deleteTargets, idArgs, the group's operation tool), so it cannot drift
// from the code. English, as the rules of the evo skill it goes with; the
// form of consent is said there, once. false for a group that is not.
func GroupCard(group string) (string, bool) {
	if !slices.Contains(Groups(), group) {
		return "", false
	}
	r := New(nil)
	var rows []string
	for _, t := range r.Tools() {
		if t.Group != group {
			continue
		}
		command := ""
		if t.CLI.Group != "" {
			command = "`evo " + t.CLI.Group + " " + t.CLI.Verb + "`"
		}
		rows = append(rows, fmt.Sprintf("| `%s` | %s | %s | %s |", t.Name, command, cardDoes(t), cardIDs(t.Name)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# evo: the `%s` group\n\n", group)
	fmt.Fprintf(&b, "<!-- Generated from the tool registry of evo: 'evo agent skill --group %s' prints it. -->\n\n", group)
	fmt.Fprintf(&b, "%d tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. "+
		"The arguments of a tool: `evo tools describe <tool> --json`.\n\n", len(rows))
	b.WriteString("| Tool | Command | Does | Ids from |\n|---|---|---|---|\n")
	b.WriteString(strings.Join(rows, "\n") + "\n")
	if _, ok := r.Lookup(group + "_operation"); ok {
		fmt.Fprintf(&b, "\nA change of this group that answers an `operation_id` runs on: wait for it with `%s_operation` "+
			"and its `wait_seconds`, not by polling a list.\n", group)
	}
	return b.String(), true
}

// cardDoes is what a tool does, as the declarations say it.
func cardDoes(t Tool) string {
	if c, ok := createdIDs[t.Name]; ok {
		var parts []string
		switch {
		case c.id != "":
			parts = append(parts, "id in `"+c.id+"`")
		case c.arg != "":
			parts = append(parts, "known by its `"+c.arg+"`")
		case c.none != "":
			parts = append(parts, c.none)
		}
		if c.list != "" {
			parts = append(parts, "found with `"+c.list+"`")
		}
		if c.delete != "" {
			parts = append(parts, "deleted with `"+c.delete+"`")
		}
		return "creates: " + strings.Join(parts, "; ")
	}
	if _, ok := deleteTargets[t.Name]; ok {
		return "deletes; a repeat answers `already_deleted`"
	}
	switch {
	case sensitiveTools[t.Name]:
		return "reveals a secret"
	case t.Mutating:
		return "changes"
	}
	return "reads"
}

// cardIDs are where the id arguments of a tool come from: "`arg` ← `list`".
func cardIDs(tool string) string {
	var out []string
	for _, a := range idArgs[tool] {
		if a.kind != nil && a.kind.listTool != "" {
			out = append(out, "`"+a.name+"` ← `"+a.kind.listTool+"`")
		}
	}
	return strings.Join(out, ", ")
}
