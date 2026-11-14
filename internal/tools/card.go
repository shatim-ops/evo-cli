// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// GroupCard is the card of a tool group for an agent (#515, option A,
// layer B): every tool of the group with its command, what it does, where
// its ids come from, what a create makes and what deletes it, and what to
// wait with — all from the registry's declarations (Decl.Created,
// Decl.Delete, Decl.IDArgs, the group's operation tool), so it cannot drift
// from the code — and the group's notes (cardNotes). English, as the rules
// of the evo skill it goes with; the form of consent is said there, once.
// false for a group that is not.
func GroupCard(group string) (string, bool) {
	return groupCard(group, true, nil, false)
}

// cardLimit is the bytes a card may take: it is read into the context of
// an agent, and the notes of a group are a line each, not a manual.
const cardLimit = 6000

// groupCard is the card of group for a terminal (the command, docs) or for
// a client of the MCP server, which has none: the same rows of the
// registry, without the commands and the pointers to `evo …` — the answer
// of a server does not name the way around that server's limits («Риск»,
// conclusion 160). served, when the server serves a part of the tools
// (--toolsets, --read-only), marks each tool it does not serve.
// bound, a server bound to its projects (--allow-project), answers a
// repeat of a delete NOT_FOUND (H9): the card says so.
func groupCard(group string, terminal bool, served func(string) bool, bound bool) (string, bool) {
	if !slices.Contains(Groups(), group) {
		return "", false
	}
	r := New(nil)
	var rows, filters []string
	missing := false
	for _, t := range r.Tools() {
		if t.Group != group {
			continue
		}
		command := ""
		if t.CLI.Group != "" {
			command = "`evo " + t.CLI.Group + " " + t.CLI.Verb + "`"
		}
		if terminal {
			rows = append(rows, fmt.Sprintf("| `%s` | %s | %s | %s |", t.Name, command, cardDoes(t, bound), cardIDs(t.Name)))
		} else {
			name := "`" + t.Name + "`"
			if served != nil && !served(t.Name) {
				name += " (not on this server)"
				missing = true
			}
			rows = append(rows, fmt.Sprintf("| %s | %s | %s |", name, cardDoes(t, bound), cardIDs(t.Name)))
		}
		if f := ListFilters(t); f != nil {
			filters = append(filters, "`"+t.Name+"` — "+strings.Join(f, ", "))
		}
	}
	if served != nil && !missing {
		// The other columns, the filters and the notes name tools of other
		// groups (deleted with destroy, ids from image_list): a server of
		// this group alone may lack them (FB19I-25).
		missing = namesUnserved(r, served, append(append(rows, filters...), cardNotes[group]...))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# evo: the `%s` group\n\n", group)
	if terminal {
		fmt.Fprintf(&b, "<!-- Generated from the tool registry of evo: 'evo agent skill --group %s' prints it. -->\n\n", group)
		fmt.Fprintf(&b, "%d tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. "+
			"The arguments of a tool: `evo tools describe <tool> --json`.\n\n", len(rows))
		b.WriteString("| Tool | Command | Does | Ids from |\n|---|---|---|---|\n")
	} else {
		fmt.Fprintf(&b, "%d tools. Consent and untrusted answers: the description of each tool; its arguments: tools/list. "+
			"A card is not a preview and no ground for confirmed=true.\n\n", len(rows))
		if missing {
			b.WriteString("What this server serves is the person's choice: a tool that is not on this server is not to be reached another way. " +
				"Tools named in the other columns may be absent too: tools/list is what this server has.\n\n")
		}
		b.WriteString("| Tool | Does | Ids from |\n|---|---|---|\n")
	}
	b.WriteString(strings.Join(rows, "\n") + "\n")
	if _, ok := r.Lookup(group + "_operation"); ok {
		fmt.Fprintf(&b, "\nA change of this group that answers an `operation_id` runs on: wait for it with `%s_operation` "+
			"and its `wait_seconds`, not by polling a list.\n", group)
	}
	if len(filters) > 0 {
		// One line: the vocabulary is the skill's (listSpecs).
		rule := " (the rule of the `evo` skill)"
		if !terminal {
			rule = ""
		}
		fmt.Fprintf(&b, "\nFilters of the lists%s: %s.\n", rule, strings.Join(filters, "; "))
	}
	if notes := cardNotes[group]; len(notes) > 0 {
		b.WriteString("\n## Notes\n\n- " + strings.Join(notes, "\n- ") + "\n")
	}
	return b.String(), true
}

// toolName is a name in backquotes, as a card writes a tool's.
var toolName = regexp.MustCompile("`([a-z0-9_]+)`")

// namesUnserved reports a tool of the registry named in lines that the
// server does not serve.
func namesUnserved(r *Registry, served func(string) bool, lines []string) bool {
	for _, line := range lines {
		for _, m := range toolName.FindAllStringSubmatch(line, -1) {
			if _, ok := r.Lookup(m[1]); ok && !served(m[1]) {
				return true
			}
		}
	}
	return false
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
		"`deploy` refuses the compose file of another deployment of its directory; the VM is `evo-<project_name>`: the project name of evo's configuration, else the directory's name.",
		"One `deploy` at a time in a directory: another answers `STATE_LOCKED`; wait for the first.",
		"The defaults of `deploy_plan` (flavor, zone, image) come from evo's configuration; the plan names what it picked.",
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
func cardDoes(t Tool, bound bool) string {
	if c, ok := createdOf(t.Name); ok {
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
	if _, ok := deleteOf(t.Name); ok {
		if bound {
			// alreadyDeleted on a server bound to its projects (H9).
			return "deletes; a repeat answers `NOT_FOUND`"
		}
		return "deletes; a repeat answers `already_deleted`"
	}
	switch {
	case IsSensitive(t):
		return "reveals a secret"
	case HasSideEffects(t):
		return t.Decl.SideEffects
	case t.Mutating:
		return "changes"
	}
	return "reads"
}

// cardIDs are where the id arguments of a tool come from: "`arg` ← `list`".
func cardIDs(tool string) string {
	var out []string
	for _, a := range decls[tool].IDArgs {
		if a.kind != nil && a.kind.listTool != "" {
			out = append(out, "`"+a.name+"` ← `"+a.kind.listTool+"`")
		}
	}
	return strings.Join(out, ", ")
}

// handleAgentSkill answers the card of a tool group, or the groups: the
// registry's own declarations, read with no engine and no request.
func (r *Registry) handleAgentSkill(ctx context.Context, args map[string]any) (string, bool) {
	group := strings.TrimSpace(stringArg(args, "group"))
	if group == "" {
		type row struct {
			Name     string `json:"name"`
			Tools    int    `json:"tools"`
			OnServer *int   `json:"on_server,omitempty"`
		}
		rows := make([]row, 0, len(Groups()))
		for _, g := range Groups() {
			x := row{Name: g}
			on := 0
			for _, t := range r.tools {
				if t.Group != g {
					continue
				}
				x.Tools++
				if r.served == nil || r.served(t.Name) {
					on++
				}
			}
			if r.served != nil {
				x.OnServer = &on
			}
			rows = append(rows, x)
		}
		return toJSON(map[string]any{"groups": rows}), false
	}
	// The command prints the card of a terminal; the server answers the
	// one without it.
	card, ok := groupCard(group, callerOf(ctx) != nil, r.served, r.projectBound)
	if !ok {
		return errResult(clierrors.New(clierrors.CodeUsage,
			i18n.Tf("unknown group %q", "неизвестная группа %q", group),
			i18n.Tf("Groups: %s.", "Группы: %s.", strings.Join(Groups(), ", ")))), true
	}
	return toJSON(map[string]any{"group": group, "skill": card}), false
}
