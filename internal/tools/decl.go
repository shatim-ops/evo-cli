// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "iter"

// Decl is the declaration of a tool: what the registry, the server and the
// invariants need to know of it besides its name, description and schema.
// Every rule of a tool is read from it, not from a list keyed by the tool's
// name (modules of other domains, step 0: docs/internal/briefs/modules.md,
// §5). The tools of evo declare it in decls; New sets Tool.Decl.
type Decl struct {
	// Group is the tool's group when its name does not start with the
	// group (groupPrefixes assign the rest).
	Group string
	// Project: the tool calls a project-scoped API on every call: a list
	// or a create of the project (project_id in the query or the body, or
	// the project in the path: monitoring, audit), or resources addressed
	// by name or path inside the project (Container Apps, Secret Manager),
	// and the mk8s catalogues (projectId is required there). Without a
	// project it answers CONFIG_INVALID before any request (release audit,
	// R-06): the API said 422 "query.project_id: Field required", and that
	// was API_ERROR, exit 5. Tools addressing a resource by its id,
	// catalogues (zones, flavors, images, Marketplace, disk types), org_*,
	// billing_*, Object Storage and the local state work without one; a
	// name a tool resolves through a compute list hits the same check
	// there (provision). The check is engineFor's: the builders of the
	// handlers' engines and clients use it (requireProject).
	Project bool
	// RequiredOneOf are argument sets one argument of each the call must
	// have, which "required" cannot say: each set is the arguments one of
	// which the call must have (vm_get: vm_id or name). The handler
	// answers USAGE without one.
	//
	// The input schema does not carry this as a top-level anyOf (QA 20.10,
	// QA-4): the Anthropic API refuses a tool whose input_schema has
	// anyOf, oneOf or allOf at the top level (400 «input_schema does not
	// support oneOf, allOf, or anyOf at the top level»), and an MCP client
	// hands it the schema as it is — one such tool fails every request of
	// the session. The property descriptions say it instead; RequiredOneOf
	// carries it to docs/tools.md and the empty-arguments test.
	RequiredOneOf [][]string
	// AlwaysListed: a server lists and serves the tool whatever its
	// --toolsets, for this reason.
	AlwaysListed string

	// Sensitive: the tool reveals a secret in plaintext (a decrypted
	// secret value, a private key). It takes confirmed, so the agent asks
	// the person first, but it changes nothing, so the rule "a tool is
	// mutating when it takes confirmed" does not apply: it is not Mutating
	// (no destructiveHint, no mutating label in the CLI). It is not marked
	// readOnlyHint either: a client may auto-approve read-only tools, and
	// the plaintext would then land in the agent's context (and its logs)
	// without the person seeing the call (security audit 19.10, SEC-4).
	Sensitive bool
	// SideEffects: the tool changes nothing the call asks to change, so it
	// takes no confirmed and is not Mutating, but it is not a read either
	// (A9, REL12-4): stack_status advances the stack it shows — its next
	// steps create resources — and k8s_kubeconfig mints a token of the
	// cluster. It is not marked readOnlyHint: a client that approves reads
	// by itself would run it unseen. The value is what the effect is: the
	// group card says it (cardDoes), and 'tools list/describe' mark the
	// tool side_effects — one decision point for MCP, the card and the
	// catalog (external review 11.11).
	SideEffects string
	// Destructive: a mutating tool, besides *_delete, that may delete or
	// overwrite data: it gets destructiveHint in MCP, so a client asks the
	// person every time. kubectl, apply and helm run whatever they are
	// given (delete, uninstall); s3_put overwrites an existing key;
	// k8s_add_addon with resolve_mode OVERWRITE replaces objects already in
	// the cluster.
	Destructive bool
	// PartialConsent: the tool takes confirmed but needs it only for the
	// calls this rule of its handler's gate says: k8s_kubectl for a command
	// that is not a read (kubectlNeedsConsent), k8s_helm for install,
	// upgrade and uninstall (helmNeedsConsent). Every call of any other
	// tool that takes confirmed needs it.
	PartialConsent func(args map[string]any) bool

	// Created: the tool is a create, and this is what names what it makes
	// (created.go).
	Created created
	// CreateName: the create names what it makes by these arguments, by
	// this rule (create_names.go, D30).
	CreateName createName
	// Nameless: the create makes a resource whose name the call does not
	// give, and why.
	Nameless string
	// Delete: the tool deletes a resource, this one
	// (TestAlreadyDeleted_EveryDeleteDeclared keeps the declarations
	// closed). stack_destroy deletes the resources of a job through these
	// tools and takes their already_deleted as gone; the job itself is
	// never deleted (it is marked destroyed), so a job that is not there is
	// a wrong id.
	Delete deleteTarget
	// IDArgs are the arguments of the tool that name a resource by id
	// (TestProjectBound_IDArgsDeclared keeps the declarations closed: a new
	// tool with such an argument fails it until the argument is declared).
	IDArgs []idArg
	// RefArgs are the reference arguments a name may stand in for (D57):
	// the registry swaps the name for its id before the handler, so the
	// checks, the preview and the question see the id, and a name the
	// project lacks is refused before them.
	RefArgs []nameRef

	// Untrusted is what of the tool's answer evo did not write: the one
	// declaration of the rule «text from the cloud is untrusted». The note
	// in the description and the marker in the answer are both made from
	// it (release plan 1.7, «Недоверенный вывод без маркера»: two lists had
	// drifted apart).
	Untrusted untrustedAnswer
	// EchoedArgs are the optional arguments a create sends as the call
	// gives them and its preview names as they are (CLAUDE.md §6, axis 4:
	// the preview names everything evo sends; release plan 1.6, «Превью
	// называет заданные необязательные аргументы»). One point for them
	// all, as namedProject is for the project, not a line in each handler.
	// A secret never goes here: the preview shows the value as the call
	// gives it, or its summary.
	EchoedArgs []echoedArg
	// Completes are the arguments of the tool whose name means a different
	// list in each product (version_id: kafka_versions in Kafka,
	// redis_versions in Redis), or that name a list valueRefs has under
	// another name (nodes_subnet_id: the subnets): Tab completes them from
	// this list, never from the one valueRefs has for the name.
	Completes map[string]completion
	// NoCompletion are the arguments named as in valueRefs that name
	// something else in the tool, with why.
	NoCompletion map[string]string
}

// deleteOf is what tool deletes, and whether it is a delete.
func deleteOf(tool string) (deleteTarget, bool) {
	d := decls[tool].Delete
	return d, d.arg != ""
}

// createdOf is the declaration of what tool creates, and whether it is a
// create.
func createdOf(tool string) (created, bool) {
	c := decls[tool].Created
	return c, c.declared()
}

// allCreated are the creates and what names what they make.
func allCreated() iter.Seq2[string, created] {
	return func(yield func(string, created) bool) {
		for name, d := range decls {
			if d.Created.declared() && !yield(name, d.Created) {
				return
			}
		}
	}
}

// createNameOf is the rule of the name of what tool makes, and whether it
// declares one.
func createNameOf(tool string) (createName, bool) {
	c := decls[tool].CreateName
	return c, len(c.args) > 0
}

// decls are the declarations of the tools of evo, in the order of
// registerTools. A tool with nothing to declare has no entry.
var decls = map[string]Decl{
	// deploy: the output of docker compose up — its log over MCP, the last
	// lines of a failure (#558).
	"deploy": {Group: "deploy", Created: created{id: "vm_id", delete: "destroy"}, Nameless: "the VM is named by the project (.evo.yaml, the directory), checked by provision.ValidName", IDArgs: []idArg{{name: "image", kind: kindImage}, {name: "image_id", kind: kindImage}, {name: "project_id", why: whyProjectArg}}, Untrusted: untrustedAnswer{fields: []string{"log", "output", "ports_open_only_to_groups"}, workload: true}},
	"provision": {Group: "vm", Project: true, Created: created{id: "vm_id", list: "vm_list", field: "vms", delete: "destroy"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}, IDArgs: []idArg{{name: "vm_id", why: whyContinued}, {name: "image", kind: kindImage}, {name: "image_id", kind: kindImage}, {name: "subnet_id", why: whyListed},
		{name: "project_id", why: whyProjectArg}}, RefArgs: []nameRef{{"subnet_id", refSubnet}}},
	"status": {Group: "deploy"},
	"list":   {Group: "deploy"},
	// Compute.
	// The names of the VM, its interfaces, groups, subnets and the rules
	// of the groups, as the cloud names them («Риск», conclusion 209, У5).
	"vm_get": {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}, IDArgs: []idArg{{name: "vm_id", kind: kindVM}}, Untrusted: untrustedAnswer{fields: []string{"name", "interfaces", "security_groups", "checks", "exposure", "image"}}},
	// The VM a power tool names in its preview and its answer, as the
	// cloud names it.
	"vm_start": {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}, IDArgs: []idArg{{name: "vm_id", kind: kindVM, target: true}}, Untrusted: untrustedAnswer{fields: []string{"preview", "name", "details"}}},
	// vm_stop deletes nothing on a disk, but it takes the service down and
	// may lose what the VM held in memory: a client asks the person every
	// time (D99, §4.3).
	"vm_stop": {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}, Destructive: true, IDArgs: []idArg{{name: "vm_id", kind: kindVM, target: true}}, Untrusted: untrustedAnswer{fields: []string{"preview", "name", "details"}}},
	// vm_reboot takes the service down for the time of the reboot (§5.2).
	"vm_reboot": {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}, Destructive: true, IDArgs: []idArg{{name: "vm_id", kind: kindVM, target: true}}, Untrusted: untrustedAnswer{fields: []string{"preview", "name", "details"}}},
	// vm_resize of a running VM: whether the guest OS restarts in it the
	// probe does not see (Г-10), so the client asks the person every time
	// (§6.5, В3: unknown is taken as a restart).
	"vm_resize":            {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}, Destructive: true, IDArgs: []idArg{{name: "vm_id", kind: kindVM, target: true}}, Untrusted: untrustedAnswer{fields: []string{"preview", "name", "details"}}},
	"vm_list":              {Project: true},
	"logs":                 {Group: "deploy", Untrusted: untrustedAnswer{fields: []string{"log"}, workload: true}},
	"destroy":              {Group: "deploy", Destructive: true, Delete: deleteTarget{arg: "vm_id", what: "VM", whRU: "ВМ"}, IDArgs: []idArg{{name: "vm_id", why: whyListed}}},
	"list_zones":           {Group: "vm"},
	"marketplace_list":     {Group: "image"},
	"list_images":          {Group: "image"},
	"image_create":         {Project: true, Created: created{id: "image.id", lost: "image_id", list: "image_list", field: "images", delete: "image_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}, IDArgs: []idArg{{name: "disk_id", kind: kindDisk}}, EchoedArgs: []echoedArg{{name: "description"}, {name: "display_name"}, {name: "user_data_template", summary: templateSummary}}},
	"fip_attach":           {Group: "vpc", IDArgs: []idArg{{name: "fip", why: whyListed}, {name: "vm", why: whyListed}}},
	"image_get":            {RequiredOneOf: [][]string{{"image", "image_id"}}, IDArgs: []idArg{{name: "image", kind: kindImage}, {name: "image_id", kind: kindImage}}},
	"image_delete":         {Delete: deleteTarget{arg: "image_id", what: "image", whRU: "образ"}, IDArgs: []idArg{{name: "image_id", kind: kindImage, target: true}}},
	"deploy_plan":          {Group: "deploy", Project: true, IDArgs: []idArg{{name: "image", kind: kindImage}, {name: "base_plan_id", why: whyLocal}}},
	"deploy_apply":         {Group: "deploy", Created: created{id: "vm_id", delete: "destroy"}, Nameless: "the VM is named by the saved plan, checked by deploy_plan", IDArgs: []idArg{{name: "plan_id", why: whyLocal}, {name: "vm_id", why: whyContinued}}},
	"list_flavors":         {Group: "vm"},
	"list_disk_types":      {Group: "disk"},
	"list_subnets":         {Group: "vpc", Project: true, IDArgs: []idArg{{name: "vpc_id", why: whyFilter}}},
	"subnet_create":        {Group: "vpc", Project: true, Created: created{id: "subnet_id", list: "list_subnets", field: "subnets", delete: "subnet_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}, IDArgs: []idArg{{name: "vpc_id", kind: kindVPC}, {name: "availability_zone_id", why: whyCatalogue}}},
	"subnet_delete":        {Group: "vpc", Delete: deleteTarget{arg: "subnet_id", what: "subnet", whRU: "подсеть"}, IDArgs: []idArg{{name: "subnet_id", kind: kindSubnet, target: true}}},
	"list_security_groups": {Group: "vpc", Project: true},
	"list_floating_ips":    {Group: "vpc", Project: true},
	// The names of the source groups of a port only groups let in: not
	// named by the caller, unlike the group of the call.
	"open_ports":  {Group: "deploy", IDArgs: []idArg{{name: "security_group", why: whyListed}}, Untrusted: untrustedAnswer{fields: []string{"open_only_to_groups", "vms"}}},
	"close_ports": {Group: "deploy", Delete: deleteTarget{arg: "ports", parent: "security_group", what: "port rule", whRU: "правило порта"}, IDArgs: []idArg{{name: "security_group", why: whyListed}}},
	// The name of the group and the descriptions of its rules, in the
	// preview and in the answer: anyone with access to the project writes
	// them.
	"sg_rule_create": {Group: "vpc", Created: created{id: "rule_id", delete: "sg_rule_delete"}, Nameless: "a rule has no name: it is known by its fields, and the same rule there answers already_exists", IDArgs: []idArg{{name: "security_group", why: whyListed}, {name: "remote_group", why: whyListed}}, Untrusted: untrustedAnswer{fields: []string{"security_group", "rule", "preview"}}},
	"sg_rule_delete": {Group: "vpc", Delete: deleteTarget{arg: "rule_id", parent: "security_group", what: "security group rule", whRU: "правило группы безопасности"}, IDArgs: []idArg{{name: "security_group", why: whyListed}, {name: "rule_id", why: whyInParent}}, Untrusted: untrustedAnswer{fields: []string{"security_group", "rule", "still_allowed_by", "still_allowed_from_groups", "vms"}}},
	// The clusters of a list by their names: anyone with access to the
	// project names a cluster (release plan 1.9, «Просроченные убывающие
	// списки»).
	"k8s_list": {Project: true, Untrusted: untrustedAnswer{fields: []string{"clusters"}}},
	// Managed Kubernetes.
	// A cluster as its service answers it, whole: its name, and the names
	// of what it holds, are what members of the project wrote. access of
	// pg_get is evo's.
	"k8s_get": {IDArgs: []idArg{{name: "cluster_id", kind: kindK8s}}, Untrusted: untrustedAnswer{whole: true}},
	// mk8s catalogues take projectId too: GET /v3/addons without it is a
	// live 400, the spec requires it on /v3/addons and productConfiguration
	// (Cursor review of PR #89).
	// The zones and the flavors of a catalogue, as the platform names
	// them: text evo did not write.
	"k8s_options":    {Project: true, Untrusted: untrustedAnswer{fields: []string{"flavors", "zones", "defaults"}}},
	"k8s_plan":       {Project: true, IDArgs: []idArg{{name: "subnet_id", why: whyListed}, {name: "cluster_sa_id", why: whyReference}}, RefArgs: []nameRef{{"subnet_id", refSubnet}}, Completes: map[string]completion{"cluster_sa_id": {list: "iam_service_accounts", field: "id"}}, NoCompletion: map[string]string{"flavor": "a control-plane flavor of k8s_options, not a VM flavor"}},
	"k8s_create":     {Project: true, Created: created{id: "cluster_id", list: "k8s_list", field: "clusters", delete: "k8s_delete"}, CreateName: createName{args: []string{"name"}, rule: "k8sNameRe: the mk8s specification"}, IDArgs: []idArg{{name: "subnet_id", why: whyListed}, {name: "cluster_sa_id", why: whyReference}}, RefArgs: []nameRef{{"subnet_id", refSubnet}}, Completes: map[string]completion{"cluster_sa_id": {list: "iam_service_accounts", field: "id"}}, NoCompletion: map[string]string{"flavor": "a control-plane flavor of k8s_options, not a VM flavor"}},
	"k8s_kubeconfig": {SideEffects: "returns a kubeconfig; mints a token of the cluster", IDArgs: []idArg{{name: "cluster_id", kind: kindK8s}}},
	// The status of the resource as its service writes it, in the refusal
	// of a delete in a state no run has shown («Риск», 181, У2): a word
	// of the cloud, not of a closed set.
	// The cluster by its name, in the target of the preview of the delete.
	"k8s_delete": {Delete: deleteTarget{arg: "cluster_id", what: "Kubernetes cluster", whRU: "кластер Kubernetes",
		statePrefix: "OBJECT_STATUS_", creatingUnseen: unseenBeforeProbe, failedUnseen: unseenBeforeProbe}, IDArgs: []idArg{{name: "cluster_id", kind: kindK8s, target: true}}, Untrusted: untrustedAnswer{fields: []string{"resource_status", "target"}}},
	// The error of an operation, as Managed Kubernetes words it: the
	// service's text, with the names of the project's resources in it.
	"k8s_operation": {IDArgs: []idArg{{name: "operation_id", kind: kindOperation}}, Untrusted: untrustedAnswer{fields: []string{"error"}}},
	"k8s_nodepools": {IDArgs: []idArg{{name: "cluster_id", kind: kindK8s}}},
	// The name of a node pool and the labels and taints of its nodes:
	// anyone with access to the project writes them.
	"k8s_nodepool_get": {IDArgs: []idArg{{name: "nodepool_id", kind: kindK8sNodePool}}, Untrusted: untrustedAnswer{fields: []string{"name", "cluster_name", "labels", "taints"}}},
	// A scale down deletes nodes with the data on their disks; the hint is
	// the tool's, not the call's, so a scale up asks too (D99; conclusion
	// 112 of «Риск»).
	// The name of the pool, in the preview and in the answer; the words
	// and the code of the platform in a failed operation (details).
	"k8s_nodepool_scale": {Destructive: true, IDArgs: []idArg{{name: "nodepool_id", kind: kindK8sNodePool, target: true}}, Untrusted: untrustedAnswer{fields: []string{"preview", "name", "details"}}},
	// The cluster of the pool by its name, in the preview of the delete;
	// the name of the pool, in the target (FB19I-9); its status in a
	// refusal.
	"k8s_nodepool_delete": {Delete: deleteTarget{arg: "nodepool_id", what: "node pool", whRU: "группа узлов", statePrefix: "OBJECT_STATUS_",
		creatingUnseen: "probe Г-12, question 16 of docs/internal/briefs/k8s-nodepools.md",
		failedUnseen:   "no run has deleted a node pool in error: probe Г-12 (docs/internal/briefs/k8s-nodepools.md; FB19I-7)"}, IDArgs: []idArg{{name: "nodepool_id", kind: kindK8sNodePool, target: true}}, Untrusted: untrustedAnswer{fields: []string{"preview", "target", "resource_status"}}},
	"k8s_add_nodepool":   {Created: created{id: "nodepool_id", list: "k8s_nodepools", field: "node_pools", listArgs: []string{"cluster_id"}, delete: "k8s_nodepool_delete"}, CreateName: createName{args: []string{"name"}, what: "node pool", whRU: "группа узлов"}, IDArgs: []idArg{{name: "cluster_id", kind: kindK8s, target: true}, {name: "nodes_subnet_id", why: whyListed}, {name: "security_group_id", kind: kindSG}}, RefArgs: []nameRef{{"nodes_subnet_id", refSubnet}, {"security_group_id", refSecurityGroup}}, Completes: map[string]completion{"nodes_subnet_id": {list: "list_subnets", field: "id"}}, NoCompletion: map[string]string{"flavor": "a node flavor of k8s_options, not a VM flavor"}},
	"k8s_addons":         {Project: true},
	"k8s_addon_versions": {Project: true},
	"k8s_add_addon":      {Project: true, Destructive: true, Created: created{none: "an addon of a cluster, known by its name; it goes with the cluster"}, Nameless: "the addon is named by the catalogue (k8s_addons)", IDArgs: []idArg{{name: "cluster_id", kind: kindK8s, target: true}}},
	"k8s_kubectl":        {PartialConsent: kubectlNeedsConsent, Destructive: true, IDArgs: []idArg{{name: "cluster_id", kind: kindK8s, target: true}}, Untrusted: untrustedAnswer{fields: []string{"output"}, workload: true}},
	// kubectl apply prints what the cluster answers: admission webhooks
	// and warnings a workload of the cluster writes.
	"k8s_apply":   {Destructive: true, IDArgs: []idArg{{name: "cluster_id", kind: kindK8s, target: true}}, Untrusted: untrustedAnswer{fields: []string{"output"}, workload: true}},
	"k8s_helm":    {PartialConsent: helmNeedsConsent, Destructive: true, IDArgs: []idArg{{name: "cluster_id", kind: kindK8s, target: true}}, Untrusted: untrustedAnswer{fields: []string{"output"}, workload: true}},
	"backup_list": {Project: true, IDArgs: []idArg{{name: "resource_id", why: whyFilter}}, Untrusted: untrustedAnswer{fields: []string{"backups"}}},
	// A backup and its source, as anyone with access to the project names
	// them (docs/internal/briefs/backups.md).
	// The plan of a backup by its name too (FB19I-21).
	"backup_get": {IDArgs: []idArg{{name: "backup_id", kind: kindBackup}}, Untrusted: untrustedAnswer{fields: []string{"name", "description", "tags", "resource_name", "source", "protection_plan"}}},
	// The names of the backup and of its source, in the preview and the
	// answer (the audit of #1544, С1).
	"backup_create": {Project: true, RequiredOneOf: [][]string{{"vm_id", "disk_id"}}, Created: created{id: "backup_id", list: "backup_list", field: "backups", delete: "backup_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}, IDArgs: []idArg{{name: "vm_id", kind: kindVM}, {name: "disk_id", kind: kindDisk}}, Untrusted: untrustedAnswer{fields: []string{"preview", "will_create", "backup", "name"}}},
	// A backup's states are lower case (BackupState): the rule of a state
	// under way, of a state being created and of a final one is the same.
	// The names of the backup, its source and the backups that depend on
	// it, in the preview.
	"backup_delete": {Delete: deleteTarget{arg: "backup_id", what: "backup", whRU: "резервная копия",
		creatingUnseen: "no run has deleted a backup still being created: the third run of probe Г-16 (docs/internal/briefs/backups.md)",
		failedUnseen:   "no run has deleted a backup in error: the third run of probe Г-16 (docs/internal/briefs/backups.md)"}, IDArgs: []idArg{{name: "backup_id", kind: kindBackup, target: true}}, Untrusted: untrustedAnswer{fields: []string{"will_delete", "target", "resource_status"}}},
	// The names of the backup, its disk and the new disk, in the preview
	// and the answer.
	"backup_restore": {Created: created{id: "disk_id", list: "disk_list", field: "disks", delete: "disk_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}, IDArgs: []idArg{{name: "backup_id", kind: kindBackup, target: true}}, Untrusted: untrustedAnswer{fields: []string{"preview", "will_create", "disk", "name"}}},
	"disk_list":      {Project: true, Completes: map[string]completion{"type": {list: "list_disk_types", field: "name"}}},
	"disk_create":    {Project: true, Created: created{id: "disk.id", lost: "disk_id", list: "disk_list", field: "disks", delete: "disk_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}, IDArgs: []idArg{{name: "attach_to", why: whyListed}}},
	// The disk and the VM the preview of disk_attach and disk_detach names,
	// as the cloud names them.
	"disk_attach": {IDArgs: []idArg{{name: "disk_id", kind: kindDisk, target: true}, {name: "vm", why: whyListed}}, Untrusted: untrustedAnswer{fields: []string{"will_attach", "attached_vms"}}},
	"disk_detach": {IDArgs: []idArg{{name: "disk_id", kind: kindDisk, target: true}, {name: "vm", why: whyListed}}, Untrusted: untrustedAnswer{fields: []string{"will_detach", "attached_vms"}}},
	"disk_resize": {IDArgs: []idArg{{name: "disk_id", kind: kindDisk, target: true}}},
	// The VMs a disk is attached to, as the cloud names them: the refusal
	// of disk_delete (D59) names them for the way out. Names, not the
	// output of a workload: no note.
	"disk_delete": {Delete: deleteTarget{arg: "disk_id", what: "disk", whRU: "диск"}, IDArgs: []idArg{{name: "disk_id", kind: kindDisk, target: true}}, Untrusted: untrustedAnswer{fields: []string{"attached_vms"}}},
	"ca_list":     {Project: true},
	"ca_get":      {Project: true},
	// Container Apps: an app is addressed by name with projectId in the
	// query (containers/openapi.yaml); its image is a container image.
	"ca_deploy": {Project: true, Created: created{arg: "name", lost: "id", list: "ca_list", field: "containers", delete: "ca_delete"}, CreateName: createName{args: []string{"name"}, rule: "caNameRe: an RFC 1123 label"}, IDArgs: []idArg{{name: "image", why: whyContainerImage}}, NoCompletion: map[string]string{"image": "a container image of Artifact Registry, not a VM image of image_list (FB150-1)"}},
	"ca_delete": {Project: true, Delete: deleteTarget{arg: "name", what: "container app", whRU: "приложение Container Apps"}},
	"ca_logs":   {Project: true, Untrusted: untrustedAnswer{fields: []string{"logs"}, workload: true}},
	// The status of the app the question of ca_start and ca_stop names, as
	// the API gives it («Риск», conclusion 142).
	"ca_start":             {Project: true, Untrusted: untrustedAnswer{fields: []string{"app_status"}}},
	"ca_stop":              {Project: true, Untrusted: untrustedAnswer{fields: []string{"app_status"}}},
	"iam_service_accounts": {Group: "iam", Project: true, IDArgs: []idArg{{name: "project_id", why: whyProjectArg}}},
	"org_projects":         {Group: "org"},
	"org_quotas":           {Group: "org", IDArgs: []idArg{{name: "project_id", why: whyProjectArg}}},
	"agent_skill":          {Group: "auth", AlwaysListed: "the card of a group is read by the agent that works with that group, whichever groups the server serves"},
	// The roles of the key, as IAM names them: anyone who may create a
	// custom role in the organization names it (A0-2).
	"whoami":              {Group: "auth", Untrusted: untrustedAnswer{fields: []string{"roles"}}},
	"doctor":              {Group: "auth"},
	"release_floating_ip": {Group: "vpc", RequiredOneOf: [][]string{{"id", "ip_address"}}, Destructive: true, Delete: deleteTarget{arg: "id", what: "floating IP", whRU: "floating IP"}, IDArgs: []idArg{{name: "id", kind: kindFIP, target: true}}},
	"snat_list":           {Project: true},
	"snat_create":         {Project: true, Created: created{id: "nat_gateway.id", lost: "nat_gateway_id", list: "snat_list", field: "nat_gateways", delete: "snat_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}, IDArgs: []idArg{{name: "availability_zone_id", why: whyCatalogue}}},
	"snat_delete":         {Delete: deleteTarget{arg: "id", what: "SNAT gateway", whRU: "SNAT-шлюз"}, IDArgs: []idArg{{name: "id", kind: kindNAT, target: true}}},
	"pg_list":             {Project: true, Untrusted: untrustedAnswer{fields: []string{"clusters"}}},
	// Managed PostgreSQL, Kafka, Redis.
	"pg_get":           {RequiredOneOf: [][]string{{"id", "cluster_id"}}, IDArgs: []idArg{{name: "id", kind: kindPG}, {name: "cluster_id", kind: kindPG}}, Untrusted: untrustedAnswer{whole: true, own: []string{"access"}}},
	"pg_create":        {Project: true, Created: created{id: "cluster_id", list: "pg_list", field: "clusters", delete: "pg_delete"}, CreateName: createName{args: []string{"name"}, what: "PostgreSQL cluster", whRU: "кластер PostgreSQL"}, IDArgs: []idArg{{name: "specification_id", why: whyCatalogue}, {name: "subnet_id", kind: kindSubnet}}, RefArgs: []nameRef{{"subnet_id", refSubnet}}, EchoedArgs: []echoedArg{{name: "description"}}, Completes: map[string]completion{"specification_id": {list: "pg_specs", field: "id"}}},
	"pg_delete":        {RequiredOneOf: [][]string{{"id", "cluster_id"}}, Delete: deleteTarget{arg: "id", what: "PostgreSQL cluster", whRU: "кластер PostgreSQL"}, IDArgs: []idArg{{name: "id", kind: kindPG, target: true}, {name: "cluster_id", kind: kindPG, target: true}}},
	"pg_operation":     {IDArgs: []idArg{{name: "operation_id", kind: kindOperation}}, Untrusted: untrustedAnswer{fields: []string{"error"}}},
	"kafka_list":       {Project: true, Untrusted: untrustedAnswer{fields: []string{"clusters"}}},
	"kafka_get":        {IDArgs: []idArg{{name: "cluster_id", kind: kindKafka}}, Untrusted: untrustedAnswer{whole: true}},
	"kafka_specs":      {IDArgs: []idArg{{name: "version_id", why: whyCatalogue}}, Untrusted: untrustedAnswer{fields: []string{"availability_zone", "specifications"}}, Completes: map[string]completion{"version_id": {list: "kafka_versions", field: "id"}}},
	"kafka_create":     {Project: true, Created: created{id: "cluster_id", list: "kafka_list", field: "clusters", delete: "kafka_delete"}, CreateName: createName{args: []string{"name"}, what: "Kafka cluster", whRU: "кластер Kafka"}, IDArgs: []idArg{{name: "specification_id", why: whyCatalogue}, {name: "version_id", why: whyCatalogue}, {name: "subnet_id", kind: kindSubnet}}, RefArgs: []nameRef{{"subnet_id", refSubnet}}, EchoedArgs: []echoedArg{{name: "description"}}, Completes: map[string]completion{"version_id": {list: "kafka_versions", field: "id"}, "specification_id": {list: "kafka_specs", field: "id"}}},
	"kafka_delete":     {Delete: deleteTarget{arg: "cluster_id", what: "Kafka cluster", whRU: "кластер Kafka"}, IDArgs: []idArg{{name: "cluster_id", kind: kindKafka, target: true}}, Untrusted: untrustedAnswer{fields: []string{"target"}}},
	"kafka_operation":  {IDArgs: []idArg{{name: "operation_id", kind: kindOperation}}, Untrusted: untrustedAnswer{fields: []string{"error"}}},
	"redis_list":       {Project: true, Untrusted: untrustedAnswer{fields: []string{"clusters"}}},
	"redis_get":        {IDArgs: []idArg{{name: "cluster_id", kind: kindRedis}}, Untrusted: untrustedAnswer{whole: true}},
	"redis_specs":      {IDArgs: []idArg{{name: "version_id", why: whyCatalogue}}, Untrusted: untrustedAnswer{fields: []string{"availability_zone", "specifications"}}, Completes: map[string]completion{"version_id": {list: "redis_versions", field: "id"}}},
	"redis_create":     {Project: true, Created: created{id: "cluster_id", list: "redis_list", field: "clusters", delete: "redis_delete"}, CreateName: createName{args: []string{"name"}, what: "Redis cluster", whRU: "кластер Redis"}, IDArgs: []idArg{{name: "specification_id", why: whyCatalogue}, {name: "version_id", why: whyCatalogue}, {name: "subnet_id", kind: kindSubnet}}, RefArgs: []nameRef{{"subnet_id", refSubnet}}, EchoedArgs: []echoedArg{{name: "description"}}, Completes: map[string]completion{"version_id": {list: "redis_versions", field: "id"}, "specification_id": {list: "redis_specs", field: "id"}}},
	"redis_delete":     {Delete: deleteTarget{arg: "cluster_id", what: "Redis cluster", whRU: "кластер Redis"}, IDArgs: []idArg{{name: "cluster_id", kind: kindRedis, target: true}}, Untrusted: untrustedAnswer{fields: []string{"target"}}},
	"redis_operation":  {IDArgs: []idArg{{name: "operation_id", kind: kindOperation}}, Untrusted: untrustedAnswer{fields: []string{"error"}}},
	"pg_databases":     {IDArgs: []idArg{{name: "cluster_id", kind: kindPG}}},
	"pg_db_create":     {Created: created{arg: "name", list: "pg_databases", field: "databases", listArgs: []string{"cluster_id"}, delete: "pg_db_delete"}, CreateName: createName{args: []string{"name"}, what: "database", whRU: "база данных"}, IDArgs: []idArg{{name: "cluster_id", kind: kindPG, target: true}}, EchoedArgs: []echoedArg{{name: "lc_collate"}, {name: "lc_ctype"}}},
	"pg_db_delete":     {Delete: deleteTarget{arg: "name", parent: "cluster_id", what: "database", whRU: "база данных"}, IDArgs: []idArg{{name: "cluster_id", kind: kindPG, target: true}}},
	"pg_users":         {IDArgs: []idArg{{name: "cluster_id", kind: kindPG}}},
	"pg_user_create":   {Created: created{arg: "name", list: "pg_users", field: "users", listArgs: []string{"cluster_id"}, delete: "pg_user_delete"}, CreateName: createName{args: []string{"name"}, what: "database user", whRU: "пользователь базы данных"}, IDArgs: []idArg{{name: "cluster_id", kind: kindPG, target: true}}},
	"pg_user_delete":   {Delete: deleteTarget{arg: "name", parent: "cluster_id", what: "database user", whRU: "пользователь базы данных"}, IDArgs: []idArg{{name: "cluster_id", kind: kindPG, target: true}}},
	"pg_backups":       {IDArgs: []idArg{{name: "cluster_id", kind: kindPG}}},
	"pg_backup_create": {Created: created{id: "backup_id", list: "pg_backups", field: "backups", listArgs: []string{"cluster_id"}}, CreateName: createName{args: []string{"name"}, what: "backup", whRU: "резервная копия"}, IDArgs: []idArg{{name: "cluster_id", kind: kindPG, target: true}}},
	"lb_list":          {Project: true, IDArgs: []idArg{{name: "vpc_id", why: whyFilter}}},
	// Network Load Balancer.
	"lb_get":       {IDArgs: []idArg{{name: "id", kind: kindNLB}}},
	"lb_operation": {IDArgs: []idArg{{name: "operation_id", kind: kindOperation}}, Untrusted: untrustedAnswer{fields: []string{"error"}}},
	"tg_list":      {Project: true, IDArgs: []idArg{{name: "vpc_id", why: whyFilter}}},
	"tg_get":       {IDArgs: []idArg{{name: "id", kind: kindTG}}},
	"tg_create":    {Project: true, RequiredOneOf: [][]string{{"vm_id", "targets"}}, Created: created{id: "tg_id", list: "tg_list", field: "target_groups", delete: "tg_delete"}, CreateName: createName{args: []string{"name"}, what: "target group", whRU: "целевая группа"}, IDArgs: []idArg{{name: "vm_id", kind: kindVM}, {name: "targets", kind: kindVM, ids: targetIDs}, {name: "interface_id", why: whyInParent}}},
	"tg_delete": {Delete: deleteTarget{arg: "id", what: "target group", whRU: "группа целей",
		statePrefix: "TARGET_GROUP_STATUS_", creatingUnseen: unseenBeforeProbe, failedUnseen: unseenBeforeProbe}, IDArgs: []idArg{{name: "id", kind: kindTG, target: true}}, Untrusted: untrustedAnswer{fields: []string{"resource_status"}}},
	"lb_create": {Project: true, Created: created{id: "lb_id", list: "lb_list", field: "load_balancers", delete: "lb_delete"}, CreateName: createName{args: []string{"name"}, what: "load balancer", whRU: "балансировщик"}, IDArgs: []idArg{{name: "target_group_id", kind: kindTG}, {name: "vpc_id", kind: kindVPC}, {name: "subnet_id", why: whyListed}}, RefArgs: []nameRef{{"subnet_id", refSubnet}}, EchoedArgs: []echoedArg{{name: "target_port"}}},
	"lb_delete": {Delete: deleteTarget{arg: "id", what: "load balancer", whRU: "балансировщик",
		statePrefix: "NLB_STATUS_", creatingUnseen: unseenBeforeProbe, failedUnseen: unseenBeforeProbe}, IDArgs: []idArg{{name: "id", kind: kindNLB, target: true}}, Untrusted: untrustedAnswer{fields: []string{"resource_status"}}},
	"vpc_list": {Project: true},
	// VPC.
	"vpc_get":       {IDArgs: []idArg{{name: "id", kind: kindVPC}}},
	"vpc_create":    {Project: true, Created: created{id: "vpc_id", list: "vpc_list", field: "vpcs", delete: "vpc_delete"}, CreateName: createName{args: []string{"name"}, what: "VPC", whRU: "VPC"}, EchoedArgs: []echoedArg{{name: "description"}}},
	"vpc_delete":    {Delete: deleteTarget{arg: "id", what: "VPC", whRU: "VPC"}, IDArgs: []idArg{{name: "id", kind: kindVPC, target: true}}},
	"vpc_operation": {IDArgs: []idArg{{name: "operation_id", kind: kindOperation}}, Untrusted: untrustedAnswer{fields: []string{"error"}}},
	"vpc_routes":    {IDArgs: []idArg{{name: "vpc_id", kind: kindVPC}}},
	"vpc_route_create": {Project: true, RequiredOneOf: [][]string{{"next_hop_vip_id", "next_hop_vm_id", "next_hop_magic_router_id"}}, Created: created{id: "route_id", list: "vpc_routes", field: "routes", listArgs: []string{"vpc_id"}, delete: "vpc_route_delete"}, Nameless: "a route is known by its destination, named by no argument", IDArgs: []idArg{{name: "vpc_id", kind: kindVPC, target: true}, {name: "next_hop_vm_id", kind: kindVM}, {name: "next_hop_interface_id", why: whyInParent},
		{name: "next_hop_vip_id", why: whyReference}, {name: "next_hop_magic_router_id", why: whyReference}}, EchoedArgs: []echoedArg{{name: "description"}}},
	"vpc_route_delete": {Delete: deleteTarget{arg: "route_id", parent: "vpc_id", what: "route", whRU: "маршрут"}, IDArgs: []idArg{{name: "vpc_id", kind: kindVPC, target: true}, {name: "route_id", why: whyInParent}}},
	"dns_zones":        {Project: true},
	"dns_zone_get":     {IDArgs: []idArg{{name: "id", kind: kindDNSZone}}},
	// DNS.
	"dns_zone_create":   {Project: true, Created: created{id: "zone_id", list: "dns_zones", field: "zones", delete: "dns_zone_delete"}, CreateName: createName{args: []string{"name", "domain"}, what: "DNS zone", whRU: "DNS-зона"}, IDArgs: []idArg{{name: "vpc_id", kind: kindVPC}}, EchoedArgs: []echoedArg{{name: "description"}}},
	"dns_zone_delete":   {Delete: deleteTarget{arg: "id", what: "DNS zone", whRU: "DNS-зона"}, IDArgs: []idArg{{name: "id", kind: kindDNSZone, target: true}}},
	"dns_operation":     {IDArgs: []idArg{{name: "operation_id", kind: kindOperation}}, Untrusted: untrustedAnswer{fields: []string{"error"}}},
	"dns_records":       {IDArgs: []idArg{{name: "zone_id", kind: kindDNSZone}}},
	"dns_record_get":    {IDArgs: []idArg{{name: "record_id", kind: kindDNSRecord}}},
	"dns_record_create": {RequiredOneOf: [][]string{{"value", "values"}}, Created: created{id: "record_id", list: "dns_records", field: "records", listArgs: []string{"zone_id"}, delete: "dns_record_delete"}, CreateName: createName{args: []string{"name"}, what: "DNS record", whRU: "DNS-запись"}, IDArgs: []idArg{{name: "zone_id", kind: kindDNSZone, target: true}}},
	"dns_record_delete": {Delete: deleteTarget{arg: "record_id", what: "DNS record", whRU: "DNS-запись"}, IDArgs: []idArg{{name: "record_id", kind: kindDNSRecord, target: true}}},
	"secret_list":       {Project: true},
	"secret_get":        {Project: true},
	"secret_value":      {Project: true, Sensitive: true},
	// Secret Manager: the create carries projectId.
	"secret_create":      {Project: true, Created: created{arg: "path", lost: "secret_id", list: "secret_list", field: "secrets", delete: "secret_delete"}, CreateName: createName{args: []string{"path"}, what: "secret", whRU: "секрет"}, IDArgs: []idArg{{name: "kms_key_id", why: whyReference}}, NoCompletion: map[string]string{"path": "the path of a new secret: an existing one is refused (D49)"}},
	"secret_add_version": {Project: true, Nameless: "a version of a secret that is there"},
	"secret_delete":      {Project: true, Delete: deleteTarget{arg: "path", what: "secret", whRU: "секрет"}},
	"ar_list":            {Project: true},
	// Artifact Registry: GET and DELETE /v1/registries/{id} take projectId
	// (required by ar/openapi.yaml); the operations do not.
	"ar_get":    {Project: true, IDArgs: []idArg{{name: "id", why: whyInProject}}},
	"ar_create": {Project: true, Created: created{id: "registry_id", list: "ar_list", field: "registries", delete: "ar_delete"}, CreateName: createName{args: []string{"name"}, what: "registry", whRU: "реестр"}},
	"ar_delete": {Project: true, Delete: deleteTarget{arg: "id", what: "artifact registry", whRU: "реестр артефактов"}, IDArgs: []idArg{{name: "id", why: whyInProject}}},
	// The error of an operation is the service's text, with the names of
	// the project's resources in it (R-83): every *_operation marks it.
	"ar_operation": {IDArgs: []idArg{{name: "operation_id", kind: kindOperation}}, Untrusted: untrustedAnswer{fields: []string{"error"}}},
	// The project itself.
	"billing_consumption": {IDArgs: []idArg{{name: "project_id", why: whyProjectArg}}},
	"billing_finops":      {IDArgs: []idArg{{name: "project_id", why: whyProjectArg}}},
	"metrics_history":     {Project: true},
	"metrics_scrape":      {Project: true},
	"audit_events":        {Project: true},
	"cert_list":           {Project: true},
	// Certificate Manager.
	"cert_get":         {IDArgs: []idArg{{name: "id", kind: kindCert}}},
	"cert_issue_le":    {Project: true, Created: created{id: "certificate_id", list: "cert_list", field: "certificates", delete: "cert_delete"}, CreateName: createName{args: []string{"name"}, what: "certificate", whRU: "сертификат"}, EchoedArgs: []echoedArg{{name: "description"}}},
	"cert_import":      {Project: true, Created: created{id: "certificate.id", lost: "certificate_id", list: "cert_list", field: "certificates", delete: "cert_delete"}, CreateName: createName{args: []string{"name"}, what: "certificate", whRU: "сертификат"}, EchoedArgs: []echoedArg{{name: "description"}, {name: "intermediate_certificate", summary: certChainSummary}}},
	"cert_renew":       {IDArgs: []idArg{{name: "certificate_id", kind: kindCert, target: true}}},
	"cert_delete":      {Delete: deleteTarget{arg: "id", what: "certificate", whRU: "сертификат"}, IDArgs: []idArg{{name: "id", kind: kindCert, target: true}}},
	"cert_usage":       {IDArgs: []idArg{{name: "id", kind: kindCert}}},
	"cert_versions":    {IDArgs: []idArg{{name: "certificate_id", kind: kindCert}}},
	"cert_private_key": {Sensitive: true, IDArgs: []idArg{{name: "certificate_id", kind: kindCert, target: true}, {name: "version_id", why: whyInParent}}},
	"cert_operation":   {IDArgs: []idArg{{name: "operation_id", kind: kindOperation}}, Untrusted: untrustedAnswer{fields: []string{"error"}}},
	// An object of S3: anyone with write access to the bucket wrote it.
	"s3_get":           {Untrusted: untrustedAnswer{fields: []string{"content", "content_base64"}, workload: true}},
	"s3_put":           {Destructive: true, Created: created{arg: "key", delete: "s3_rm"}, Nameless: "an object key may hold spaces anywhere: S3 takes any UTF-8"},
	"s3_rm":            {Destructive: true, Delete: deleteTarget{arg: "key", parent: "bucket", what: "object", whRU: "объект"}},
	"s3_bucket_create": {Created: created{arg: "bucket", delete: "s3_bucket_delete"}, CreateName: createName{args: []string{"bucket"}, rule: "bucketNameRe and the S3 client's rule"}},
	"s3_bucket_delete": {Delete: deleteTarget{arg: "bucket", what: "bucket", whRU: "бакет"}},
	// Stack jobs.
	// The names of the fields a manifest of stack_plan has and its type
	// does not: the caller's text, which a manifest a cloned repository
	// brings may write (D108; «Риск», 186, У8).
	"stack_plan": {Untrusted: untrustedAnswer{fields: []string{"unknown_fields", "error"}}},
	// The steps of the job in the question of stack_apply: the names and
	// the values of a manifest, the caller's text a cloned repository may
	// bring, as stack_plan's.
	"stack_apply":   {IDArgs: []idArg{{name: "job_id", why: whyLocal}}, Untrusted: untrustedAnswer{fields: []string{"preview"}}},
	"stack_status":  {SideEffects: "advances the running stack: its next steps create the resources approved in stack_apply; on a --read-only server only shows it", IDArgs: []idArg{{name: "job_id", why: whyLocal}}},
	"stack_destroy": {Destructive: true, IDArgs: []idArg{{name: "job_id", why: whyLocal}}},
}
