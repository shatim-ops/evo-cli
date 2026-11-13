// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// skillPlace is where an item of a scenario skill lives once D68 takes
// skills/ away: in the description of a tool (with its schema), in the card
// of a group, in the built-in skill evo — with a phrase of it the text
// there holds — or nowhere, with why.
type skillPlace struct {
	tool, card string // a tool of the registry, a group of GroupCard
	builtin    bool   // the built-in skill (evo agent skill)
	phrase     string // what the text there says of it, case aside
	why        string // why it is not kept: a skill's routing, a rule D68 drops
}

// skillKnowledge are the items of the scenario skills (evo-deploy,
// evo-postgres, evo-scenarios, evo-vm; not evo-cleanup and the Cleanup
// sections, which D68 drops: owner's decisions 03.11) that have their place:
// "skill§section§the first 60 characters of the item" → the place.
var skillKnowledge = map[string]skillPlace{
	"evo-deploy§Use this skill when§The person wants an app from a `docker-compose.yml` running ":                                 {tool: "deploy", phrase: "Deploy the local compose project to a VM"},
	"evo-deploy§Use this skill when§Not for a bare VM, ports or a public IP alone: use `evo-vm`.":                                 {why: whyRouting},
	"evo-deploy§Use this skill when§Not for Kubernetes: this set has no skill for it.":                                            {why: whyRouting},
	"evo-deploy§Prerequisites§`evo doctor --json` (tool `doctor`, group `auth`) passes: ke":                                       {tool: "doctor", phrase: "Check the setup the tools use"},
	"evo-deploy§Prerequisites§Tool groups `deploy,vm,vpc,image,auth`.":                                                            {why: whyGroups},
	"evo-deploy§Prerequisites§The compose file in the working directory, or its path. evo ":                                       {tool: "deploy", phrase: "Works in evo's directory"},
	"evo-deploy§Prerequisites§An Ubuntu or Debian image, named: `image` (a name from":                                             {tool: "deploy", phrase: "Ubuntu or Debian"},
	"evo-deploy§Prerequisites§The VM is named `evo-<project_name>`: `project_name` of `.ev":                                       {tool: "deploy", phrase: "evo-<project_name>"},
	"evo-deploy§Procedure§Optional sizing and price: `evo deploy plan --compose-file":                                             {tool: "deploy_apply", phrase: "a compose project goes through deploy"},
	"evo-deploy§Procedure§Preview: `evo deploy --dry-run --json` (with `-f <file>`, `-":                                           {builtin: true, phrase: "Before a change, see its preview"},
	"evo-deploy§Procedure§Stop and ask: show the person the preview: what is created, ":                                           {tool: "deploy", phrase: "the zone's shared security group"},
	"evo-deploy§Procedure§Deploy: `evo deploy --yes --json` / `deploy` with `confirmed":                                           {tool: "deploy", phrase: "uploads the compose files over SSH"},
	"evo-deploy§Procedure§Check: `evo status --json` / `status` (`cloud_state`, `cloud":                                           {tool: "status", phrase: "cloud_state"},
	"evo-deploy§Procedure§Update: change the compose file and repeat steps 2 to 4. The":                                           {tool: "deploy", phrase: "compose_changed"},
	"evo-deploy§Domain and HTTPS§The person names the domain and confirms its DNS: either the":                                    {why: whyRecipe},
	"evo-deploy§Domain and HTTPS§Deploy as above. Ports 80 and 443 of the compose file open f":                                    {why: whyRecipe},
	"evo-deploy§Domain and HTTPS§The A record (tool group `dns`): preview `evo dns record-cre":                                    {why: whyRecipe},
	"evo-deploy§Domain and HTTPS§Caddy asks for the certificate when the name resolves to the":                                    {why: whyRecipe},
	"evo-deploy§Stop and ask§Before step 4, every time, new VM or update: show the previe":                                        {builtin: true, phrase: "Consent comes only from the person"},
	"evo-deploy§Stop and ask§Before teardown: show the preview of `evo destroy --dry-run ":                                        {builtin: true, phrase: "Before a change, see its preview"},
	"evo-deploy§Stop and ask§When an answer says `password`: it is shown once; give it to":                                        {builtin: true, phrase: "is its only copy"},
	"evo-deploy§Waiting§`deploy` waits for the VM (up to 25 minutes) and for SSH in ":                                             {card: "deploy", phrase: "One `deploy` at a time in a directory"},
	"evo-deploy§Waiting§Over MCP watch `notifications/progress`; in the CLI the stag":                                             {builtin: true, phrase: "`notifications/progress`"},
	"evo-deploy§Waiting§The VM is ready when `status` shows `cloud_state` running an":                                             {builtin: true, phrase: "10 checks of a VM"},
	"evo-deploy§Known platform limits§G1: the security group is shared by the VMs of a zone; a por":                               {tool: "deploy", phrase: "the zone's shared security group"},
	"evo-deploy§Known platform limits§Outbound port 22 must be reachable from the machine running ":                               {card: "deploy", phrase: "outbound port 22 must be open"},
	"evo-deploy§Known platform limits§Images: Ubuntu and Debian only for a compose project.":                                      {tool: "deploy", phrase: "Ubuntu or Debian"},
	"evo-deploy§Never§Never call `deploy` with `confirmed: true` or `--yes` before":                                               {builtin: true, phrase: "Consent comes only from the person"},
	"evo-deploy§Never§Never use `deploy_apply` for a compose project.":                                                            {tool: "deploy_apply", phrase: "a compose project goes through deploy"},
	"evo-deploy§Never§Never ask for the yes without saying that the ports open fro":                                               {tool: "deploy", phrase: "the zone's shared security group"},
	"evo-deploy§Never§Never run `deploy` from another directory to touch this depl":                                               {tool: "deploy", phrase: "Works in evo's directory"},
	"evo-deploy§Never§Never repeat a password from an answer in the chat.":                                                        {builtin: true, phrase: "repeat it in the chat, a command line or a log"},
	"evo-postgres§Use this skill when§The person wants a Managed PostgreSQL cluster, a user or a d":                               {tool: "pg_create", phrase: "Create a Managed PostgreSQL cluster"},
	"evo-postgres§Use this skill when§Not for PostgreSQL inside a VM or a compose project: use `ev":                               {why: whyRouting},
	"evo-postgres§Prerequisites§`evo doctor --json` (tool `doctor`, group `auth`) passes.":                                        {tool: "doctor", phrase: "Check the setup the tools use"},
	"evo-postgres§Prerequisites§Tool groups `pg,vpc,auth`. The default of `evo agent setup` ":                                     {builtin: true, phrase: "groups `deploy,vm,vpc,disk,image,auth` by default"},
	"evo-postgres§Prerequisites§The subnet: by default the main subnet of the profile's zone":                                     {tool: "pg_create", phrase: "the main subnet of the profile's zone"},
	"evo-postgres§Procedure§Versions: `evo pg versions --json` / `pg_versions`. Specific":                                         {builtin: true, phrase: "A size is the person's decision"},
	"evo-postgres§Procedure§Preview: `evo pg create <name> --specification-id <id> --dry":                                         {card: "pg", phrase: "an estimate by the tariff of its date"},
	"evo-postgres§Procedure§Stop and ask: show the person the configuration, storage, su":                                         {tool: "pg_create", phrase: "CANNOT be changed after creation"},
	"evo-postgres§Procedure§Create: `evo pg create <name> --specification-id <id> --yes ":                                         {card: "pg", phrase: "wait for it with `pg_operation`"},
	"evo-postgres§Procedure§Wait: `evo pg operation <operation-id> --wait-seconds 300 --":                                         {card: "pg", phrase: "wait for it with `pg_operation`"},
	"evo-postgres§Procedure§User: first check `evo pg users <cluster-id> --json` / `pg_u":                                         {tool: "pg_user_create", phrase: "The preview does not check the name is free (pg_users)"},
	"evo-postgres§Procedure§Database: first check `evo pg databases <cluster-id> --json`":                                         {tool: "pg_db_create", phrase: "The preview does not check the name is free (pg_databases)"},
	"evo-postgres§Procedure§Hand over: the `connection_string` with the user and the dat":                                         {tool: "pg_get", phrase: "connection_string"},
	"evo-postgres§Stop and ask§Before every call with `confirmed: true` or `--yes`: `pg_cre":                                      {builtin: true, phrase: "Consent comes only from the person"},
	"evo-postgres§Stop and ask§Before `pg_delete`: the preview warns that the databases and":                                      {tool: "pg_delete", phrase: "including all its backups (they cannot be restored)"},
	"evo-postgres§Waiting§A cluster takes minutes. One `pg_operation` call waits up to":                                           {tool: "pg_operation", phrase: "answer the final state in one call"},
	"evo-postgres§Waiting§Read the state with `pg_get`, not by scanning `pg_list`.":                                               {card: "pg", phrase: "not by polling a list"},
	"evo-postgres§Waiting§After 20 waits with no change of state, or a cluster still `":                                           {builtin: true, phrase: "20 of a cluster"},
	"evo-postgres§Known platform limits§DB-21: a cluster in FAILED cannot be deleted through the API":                             {card: "pg", phrase: "cannot be deleted through the API (DB-21)"},
	"evo-postgres§Known platform limits§A cluster `pg_get` reads as ERASED or DELETED is gone: `pg_d":                             {builtin: true, phrase: "A delete of what is not there succeeds"},
	"evo-postgres§Known platform limits§DB-22: listing the backups of a deleted cluster answers an e":                             {card: "pg", phrase: "answers an error (DB-22)"},
	"evo-postgres§Known platform limits§DB-23: a zone without capacity refuses `pg_create` with":                                  {card: "pg", phrase: "`ResourceExhausted`, DB-23"},
	"evo-postgres§Known platform limits§Backups are set at creation and cannot be turned on or off l":                             {tool: "pg_create", phrase: "CANNOT be changed after creation"},
	"evo-postgres§Never§Never pass `confirmed: true` or `--yes` before the person sa":                                             {builtin: true, phrase: "Consent comes only from the person"},
	"evo-postgres§Never§Never put a password in your reply, a command line or a log.":                                             {builtin: true, phrase: "repeat it in the chat, a command line or a log"},
	"evo-postgres§Never§Never call `pg_create` again after an interruption before ch":                                             {tool: "pg_create", phrase: "Refuses a taken name"},
	"evo-postgres§Never§Never treat an answer to a delete of a FAILED cluster as suc":                                             {card: "pg", phrase: "not a success"},
	"evo-scenarios§2. Which skill§Kafka: evo has no tools for topics and users — the console m":                                   {card: "kafka", phrase: "no tools for topics and users"},
	"evo-scenarios§2. Which skill§Redis: `type` `STANDALONE` for one host; the default `MASTER":                                   {tool: "redis_create", phrase: "three billed hosts per shard"},
	"evo-scenarios§2. Which skill§Kubernetes: `k8s_plan`, `k8s_create`, then `k8s_add_nodepool":                                   {tool: "k8s_add_nodepool", phrase: "a fresh cluster has none"},
	"evo-scenarios§4. Long operations§A VM is created in up to 25 minutes; `deploy`, `deploy_apply":                               {builtin: true, phrase: "answers within ~45 s with its state so"},
	"evo-scenarios§4. Long operations§Asynchronous creates and deletes (PostgreSQL and other clust":                               {builtin: true, phrase: "answers an `operation_id` runs on"},
	"evo-scenarios§4. Long operations§Over MCP the progress comes as `notifications/progress` when":                               {builtin: true, phrase: "over HTTP only on an SSE stream"},
	"evo-scenarios§4. Long operations§Read the state of the resource with its `*_get` (or `status`":                               {builtin: true, phrase: "Read the state of one resource with its `*_get`"},
	"evo-scenarios§4. Long operations§After 10 calls for a VM, or 20 for a cluster, with no change":                               {builtin: true, phrase: "No change of state after 10 checks of a VM, or 20 of a cluster"},
	"evo-scenarios§7. Sizing: you estimate, evo shows the catalogue§Say what the workload needs (cpu, memory, disk, nodes) and i": {builtin: true, phrase: "A size is the person's decision"},
	"evo-scenarios§7. Sizing: you estimate, evo shows the catalogue§Read the catalogue, not your memory: `list_flavors` with fil": {builtin: true, phrase: "Read the catalogue, not your memory"},
	"evo-scenarios§7. Sizing: you estimate, evo shows the catalogue§Compare with a tool: a plan (`deploy_plan`, `k8s_plan`, `sta": {builtin: true, phrase: "Compare with a plan"},
	"evo-scenarios§7. Sizing: you estimate, evo shows the catalogue§What evo chose itself is in the preview: show it, stop and a": {builtin: true, phrase: "What evo chose itself is in the preview"},
	"evo-vm§Use this skill when§The person wants a bare VM, a port opened or closed, a publi":                                     {tool: "provision", phrase: "Provision a bare VM"},
	"evo-vm§Use this skill when§Not for an app from a compose file: use `evo-deploy`.":                                            {tool: "provision", phrase: "docker-compose: use 'deploy'"},
	"evo-vm§Prerequisites§`evo doctor --json` (tool `doctor`, group `auth`) passes.":                                              {tool: "doctor", phrase: "Check the setup the tools use"},
	"evo-vm§Prerequisites§Tool groups `vm,deploy,vpc,image,auth`: `open_ports`, `close":                                           {card: "deploy", phrase: "| `open_ports` | `evo vpc open-ports` |"},
	"evo-vm§Prerequisites§The image by name: `evo image list ubuntu --json` / `image_l":                                           {builtin: true, phrase: "call `image_list` only with `query`"},
	"evo-vm§Prerequisites§Run evo in one working directory: the VM's deploy key and re":                                           {tool: "provision", phrase: "state_dir (key, state)"},
	"evo-vm§Procedure§Preview: `evo vm create <name> --image Ubuntu-24.04 --dry-ru":                                               {tool: "provision", phrase: "smallest lowcost, not free-tier"},
	"evo-vm§Procedure§Do not pass ports to `provision` (`--port`): it has no `cidr":                                               {why: "stale: provision has cidr since 1.7 (P5)"},
	"evo-vm§Procedure§Stop and ask: show the preview: what is created, the price, ":                                               {builtin: true, phrase: "show the preview to the person"},
	"evo-vm§Procedure§Create: `evo vm create <name> --image Ubuntu-24.04 --yes --j":                                               {tool: "provision", phrase: "password (generated; this answer only)"},
	"evo-vm§Procedure§Open a port: ask the person which source may reach it (a CID":                                               {tool: "open_ports", phrase: "ask the person which source"},
	"evo-vm§Procedure§Close a port: `evo vpc close-ports <group> --port 8080 --cid":                                               {tool: "close_ports", phrase: "Only rules of exactly that port, protocol and source go"},
	"evo-vm§Procedure§Public address: `evo vm get <name> --json` / `vm_get` gives ":                                               {tool: "fip_attach", phrase: "Pick a free address from list_floating_ips"},
	"evo-vm§Stop and ask§Before every call with `confirmed: true` or `--yes`: `provis":                                            {builtin: true, phrase: "Consent comes only from the person"},
	"evo-vm§Stop and ask§`open_ports` from `0.0.0.0/0` needs its own yes, named as su":                                            {tool: "open_ports", phrase: "ask the person which source"},
	"evo-vm§Waiting§`provision` waits for the VM in the call. Over MCP watch":                                                     {builtin: true, phrase: "Over MCP a long call (a VM, a deploy) answers within ~45 s"},
	"evo-vm§Waiting§If `vm_get` shows the same state for 10 checks, stop and ask":                                                 {builtin: true, phrase: "10 checks of a VM"},
	"evo-vm§Known platform limits§G1: a security group is shared by the VMs of a zone; a rule ":                                   {tool: "open_ports", phrase: "security groups are shared per availability zone"},
	"evo-vm§Known platform limits§The generated password is returned once, in the create answe":                                   {tool: "provision", phrase: "password (generated; this answer only)"},
	"evo-vm§Never§Never pass `confirmed: true` or `--yes` before the person sa":                                                   {builtin: true, phrase: "Consent comes only from the person"},
	"evo-vm§Never§Never open a port from `0.0.0.0/0` without the person's sepa":                                                   {tool: "open_ports", phrase: "ask the person which source"},
	"evo-vm§Never§Never repeat a password from an answer in the chat.":                                                            {builtin: true, phrase: "repeat it in the chat, a command line or a log"},
	"evo-vm§Never§Never delete a VM found only by a name several VMs share: us":                                                   {tool: "destroy", phrase: "a name several VMs share is refused"},
}

// The reasons an item of a scenario skill is not kept.
const (
	whyRouting = "routing of the scenario skills, which D68 removes"
	whyGroups  = "the tool groups of a scenario skill, which D68 removes; the groups are the table of the built-in skill"
	whyRecipe  = "a recipe of a task (a domain with HTTPS through Caddy): D68 keeps no recipes; that HTTPS ends on the VM is said by lb_create"
)

// skillKnowledgePending are the items with no place yet, in the same form.
// It only shrinks. Empty: its deadline 1.7 is met, and a new line comes with
// a deadline of its own and its plan line (release plan 1.7, block D, D68:
// the facts of the platform from the scenario skills go into the
// descriptions of the registry and the built-in skill; the PR of D68 moves
// each line to skillKnowledge with its place, or with why it is dropped).
var skillKnowledgePending = map[string]string{}

// skillItem is an item of a list of a SKILL.md: "- " or "1. ".
var skillItem = regexp.MustCompile(`^(- |\d+\. )(.*)`)

// skillItemsOf are the items of the scenario skills in skills/, in the form
// of skillKnowledge's keys; nil once skills/ is gone.
func skillItemsOf(t *testing.T) map[string]bool {
	t.Helper()
	root := filepath.Join("..", "..", "skills")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	out := map[string]bool{}
	for _, sk := range []string{"evo-deploy", "evo-postgres", "evo-scenarios", "evo-vm"} {
		data, err := os.ReadFile(filepath.Join(root, sk, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		sec := ""
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "## ") {
				sec = strings.TrimSpace(line[3:])
				continue
			}
			m := skillItem.FindStringSubmatch(line)
			if m == nil || strings.Contains(sec, "Cleanup") {
				continue
			}
			fp := []rune(strings.Join(strings.Fields(m[2]), " "))
			if len(fp) > 60 {
				fp = fp[:60]
			}
			out[sk+"§"+sec+"§"+string(fp)] = true
		}
	}
	return out
}

// D68 (release plan 1.7, block D; the line made precise in #753): what the
// scenario skills knew is not lost when skills/ goes. Every item of them —
// the inventory below, taken while skills/ is there — has its place: the
// description of a tool, the card of its group or the built-in skill evo,
// whose text holds the item's phrase; or why it is dropped. While skills/
// is there, the inventory is its items: an item added or changed there is
// a line here. The bytes the moved text costs in tools/list are the
// ratchet of TestToolsList_EachToolWithinItsBudget (internal/mcp).
func TestSkills_KnowledgeHasItsPlace(t *testing.T) {
	var bad []string
	if items := skillItemsOf(t); items != nil {
		for k := range items {
			_, placed := skillKnowledge[k]
			_, pending := skillKnowledgePending[k]
			if !placed && !pending {
				bad = append(bad, k+": an item of skills/ not in the inventory — a line of skillKnowledgePending")
			}
		}
		for k := range skillKnowledge {
			if !items[k] {
				bad = append(bad, k+": in skillKnowledge, not an item of skills/")
			}
		}
		for k := range skillKnowledgePending {
			if !items[k] {
				bad = append(bad, k+": in skillKnowledgePending, not an item of skills/")
			}
		}
	}
	descs := map[string]string{}
	for _, tl := range tools.New(nil).Tools() {
		descs[tl.Name] = tl.Description + "\n" + schemaText(tl.InputSchema)
	}
	builtin := ""
	for k, p := range skillKnowledge {
		if _, ok := skillKnowledgePending[k]; ok {
			bad = append(bad, k+": both placed and pending")
		}
		var text string
		switch {
		case p.why != "" && p.tool == "" && p.card == "" && !p.builtin:
			continue
		case p.tool != "":
			d, ok := descs[p.tool]
			if !ok {
				bad = append(bad, k+": placed in tool "+p.tool+", no such tool")
				continue
			}
			text = d
		case p.card != "":
			c, ok := tools.GroupCard(p.card)
			if !ok {
				bad = append(bad, k+": placed in the card of "+p.card+", no such group")
				continue
			}
			text = c
		case p.builtin:
			if builtin == "" {
				builtin = agentSkillBody(newRootCmd(), false)
			}
			text = builtin
		default:
			bad = append(bad, k+": a place without a tool, a card, the built-in skill or why")
			continue
		}
		if p.phrase == "" || !strings.Contains(strings.ToLower(text), strings.ToLower(p.phrase)) {
			bad = append(bad, k+": its place does not hold its phrase "+`"`+p.phrase+`"`)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	t.Logf("skill items: %d placed, %d pending", len(skillKnowledge), len(skillKnowledgePending))
}

// schemaText is every description of the arguments of a schema.
func schemaText(schema map[string]any) string {
	var b strings.Builder
	props, _ := schema["properties"].(map[string]any)
	for _, p := range props {
		if pm, ok := p.(map[string]any); ok {
			if d, ok := pm["description"].(string); ok {
				b.WriteString(d + "\n")
			}
		}
	}
	return b.String()
}
