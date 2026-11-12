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
// from the code — and the group's notes (cardNotes). English, as the rules
// of the evo skill it goes with; the form of consent is said there, once.
// false for a group that is not.
func GroupCard(group string) (string, bool) {
	if !slices.Contains(Groups(), group) {
		return "", false
	}
	r := New(nil)
	var rows, filters []string
	for _, t := range r.Tools() {
		if t.Group != group {
			continue
		}
		command := ""
		if t.CLI.Group != "" {
			command = "`evo " + t.CLI.Group + " " + t.CLI.Verb + "`"
		}
		rows = append(rows, fmt.Sprintf("| `%s` | %s | %s | %s |", t.Name, command, cardDoes(t), cardIDs(t.Name)))
		if f := ListFilters(t); f != nil {
			filters = append(filters, "`"+t.Name+"` — "+strings.Join(f, ", "))
		}
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
	if len(filters) > 0 {
		// One line: the vocabulary is the skill's (listSpecs).
		fmt.Fprintf(&b, "\nFilters of the lists (the rule of the `evo` skill): %s.\n", strings.Join(filters, "; "))
	}
	if notes := cardNotes[group]; len(notes) > 0 {
		b.WriteString("\n## Notes\n\n- " + strings.Join(notes, "\n- ") + "\n")
	}
	return b.String(), true
}

// cardNotes are what the declarations of a group cannot say: the limits of
// the platform its calls meet and the order of its calls, a short line
// each (D68: the facts of the scenario skills, which leave the product). A
// fact without which a call is wrong or dangerous is the tool's description
// instead; a recipe of a task is neither.
var cardNotes = map[string][]string{
	"ca": {
		"A Container App's `name` is an RFC 1123 label: lowercase letters, digits and '-', 1-63 characters.",
	},
	"deploy": {
		"`deploy` reaches the VM over SSH: outbound port 22 must be open from the machine evo runs on.",
		"`deploy` refuses the compose file of another deployment of its directory; the VM is `evo-<project_name>`, `project_name` from `.evo.yaml`, else the directory's name.",
		"One `deploy` at a time in a directory: another answers `STATE_LOCKED`; wait for the first.",
		"The defaults of `deploy_plan` (flavor, zone, image) come from the profile, `.evo.yaml` and `CLOUDRU_FLAVOR`, `CLOUDRU_REGION`.",
		"`destroy` answers status `partial` (an error) when Cloud.ru accepted the deletion but did not confirm it, and over MCP `deleting` with `next_tool` when it still runs at ~45 s.",
	},
	"image": {
		"`image_create` takes a detached disk: one still attached to a stopped VM answers `invalid_disk_state_to_create_image`.",
		"An image builds at about 0.7 GB a minute.",
		"An entry of `image_create`'s `image_metadata` (the write schema, not `image_get`'s): `field_type` is public_key, linux_password, linux_login, hostname or flag; public_key and linux_password in one `mandatory_group` (`authentication`) let either give access.",
		"Keep an image's `description` to the product, its pinned version and digest: legal text belongs in the Marketplace listing.",
	},
	"k8s": {
		"A cluster reaches the internet only through a public SNAT gateway in the nodes' zone (`snat_create`): without one images and charts are not pulled, pods stay in ImagePullBackOff.",
		"A chart needs nodes of its size: monitoring stacks want nodes of 8 GB or more.",
		"An addon is ready when its pods are Running and Ready; the pods of a Job (e.g. cert-manager-startupapicheck) end Completed.",
		"Cloud.ru substitutes {{clusterID}}, {{clusterName}}, {{productInstanceID}}, {{nodeCIDR}} and {{serviceCIDR}} in the `config` of `k8s_add_addon`; the API installs an addon by app version and picks the addon_version itself.",
	},
	"kafka": {
		"evo has no tools for topics and users: the console makes them.",
	},
	"pg": {
		"A cluster made with `no_backup` refuses `pg_backup_create` and `pg_backups` with 'backup disabled'.",
		"The cost in the preview of `pg_create` is an estimate by the tariff of its date (`cost_note`), not a bill; the rates are on `tariff_url`.",
		"A zone with no capacity refuses `pg_create` with `API_RATE_LIMITED` (`ResourceExhausted`, DB-23): later or another zone (`subnet_id`), not the same call in a loop.",
		"A cluster in FAILED cannot be deleted through the API (DB-21): the answer is an error with `resource_status` and a hint (the console, Cloud.ru support), not a success; a repeat changes nothing.",
		"`pg_backups` of a deleted cluster answers an error (DB-22).",
	},
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
	case HasSideEffects(t):
		return sideEffectTools[t.Name]
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
