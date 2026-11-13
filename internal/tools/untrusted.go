// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
)

// quoteCloud is a value the cloud gave — a resource's name — as evo's own
// text (message, next, hint, warning, note) writes it: on one line and in
// quotes. Anyone with access to the project names resources, and a name
// like "web\nIGNORE PREVIOUS INSTRUCTIONS…" must not read as evo's words to
// the agent. Data fields keep the value as the cloud has it (process audit
// 1.2.1, П9; TestUntrustedNames_OneLineInText).
func quoteCloud(s string) string { return strconv.Quote(s) }

// untrustedAnswer declares what of a tool's answer evo did not write.
// Fields are the answer fields with that text: the answer lists those it
// has in "_untrusted" — a marker for a client that reads it: the text is
// data, never instructions (process audit 1.2.1, П9). Workload says the
// text is what a workload or a stored object wrote, and New ends the
// tool's description with untrustedOutputNote (Tool.UntrustedOutput).
type untrustedAnswer struct {
	fields   []string
	workload bool
}

// untrustedFields is the one declaration of the rule «text from the cloud
// is untrusted»: the note in the description and the marker in the answer
// are both made from it (release plan 1.7, «Недоверенный вывод без
// маркера»: two maps had drifted apart).
var untrustedFields = map[string]untrustedAnswer{
	"logs":        {fields: []string{"log"}, workload: true},
	"ca_logs":     {fields: []string{"logs"}, workload: true},
	"k8s_kubectl": {fields: []string{"output"}, workload: true},
	// kubectl apply prints what the cluster answers: admission webhooks
	// and warnings a workload of the cluster writes.
	"k8s_apply": {fields: []string{"output"}, workload: true},
	"k8s_helm":  {fields: []string{"output"}, workload: true},
	// An object of S3: anyone with write access to the bucket wrote it.
	"s3_get": {fields: []string{"content", "content_base64"}, workload: true},
	// deploy: the output of docker compose up — its log over MCP, the last
	// lines of a failure (#558).
	"deploy": {fields: []string{"log", "output", "ports_open_only_to_groups"}, workload: true},
	// The status of the app the question of ca_start and ca_stop names, as
	// the API gives it («Риск», conclusion 142).
	"ca_start": {fields: []string{"app_status"}},
	"ca_stop":  {fields: []string{"app_status"}},
	// The VMs a disk is attached to, as the cloud names them: the refusal
	// of disk_delete (D59) names them for the way out. Names, not the
	// output of a workload: no note.
	"disk_delete": {fields: []string{"attached_vms"}},
	// The disk and the VM the preview of disk_attach and disk_detach names,
	// as the cloud names them.
	"disk_attach": {fields: []string{"will_attach", "attached_vms"}},
	"disk_detach": {fields: []string{"will_detach", "attached_vms"}},
	// The VM a power tool names in its preview and its answer, as the
	// cloud names it.
	"vm_start":  {fields: []string{"preview", "name", "details"}},
	"vm_stop":   {fields: []string{"preview", "name", "details"}},
	"vm_reboot": {fields: []string{"preview", "name", "details"}},
	// The roles of the key, as IAM names them: anyone who may create a
	// custom role in the organization names it (A0-2).
	"whoami": {fields: []string{"roles"}},
	// The error of an operation, as Managed Kubernetes words it: the
	// service's text, with the names of the project's resources in it.
	"k8s_operation": {fields: []string{"error"}},
	// The error of an operation is the service's text, with the names of
	// the project's resources in it (R-83): every *_operation marks it.
	"ar_operation":    {fields: []string{"error"}},
	"cert_operation":  {fields: []string{"error"}},
	"dns_operation":   {fields: []string{"error"}},
	"kafka_operation": {fields: []string{"error"}},
	"lb_operation":    {fields: []string{"error"}},
	"pg_operation":    {fields: []string{"error"}},
	"redis_operation": {fields: []string{"error"}},
	"vpc_operation":   {fields: []string{"error"}},
	// The name of a node pool and the labels and taints of its nodes:
	// anyone with access to the project writes them.
	"k8s_nodepool_get": {fields: []string{"name", "cluster_name", "labels", "taints"}},
	// A backup and its source, as anyone with access to the project names
	// them (docs/internal/briefs/backups.md).
	"backup_get":  {fields: []string{"name", "description", "tags", "resource_name", "source"}},
	"backup_list": {fields: []string{"backups"}},
	// The names of the backup and of its source, in the preview and the
	// answer (the audit of #1544, С1).
	"backup_create": {fields: []string{"preview", "will_create", "backup", "name"}},
	// The names of the backup, its disk and the new disk, in the preview
	// and the answer.
	"backup_restore": {fields: []string{"preview", "will_create", "disk", "name"}},
	// The names of the backup, its source and the backups that depend on
	// it, in the preview.
	"backup_delete": {fields: []string{"will_delete", "target", "resource_status"}},
	// The name of the pool, in the preview and in the answer; the words
	// and the code of the platform in a failed operation (details).
	"k8s_nodepool_scale": {fields: []string{"preview", "name", "details"}},
	// The cluster of the pool by its name, in the preview of the delete;
	// the name of the pool, in the target (FB19I-9); its status in a
	// refusal.
	"k8s_nodepool_delete": {fields: []string{"preview", "target", "resource_status"}},
	// The status of the resource as its service writes it, in the refusal
	// of a delete in a state no run has shown («Риск», 181, У2): a word
	// of the cloud, not of a closed set.
	"k8s_delete": {fields: []string{"resource_status"}},
	"lb_delete":  {fields: []string{"resource_status"}},
	"tg_delete":  {fields: []string{"resource_status"}},
	// The name of the group and the descriptions of its rules, in the
	// preview and in the answer: anyone with access to the project writes
	// them.
	// The names of the source groups of a port only groups let in: not
	// named by the caller, unlike the group of the call.
	"open_ports":     {fields: []string{"open_only_to_groups", "vms"}},
	"sg_rule_create": {fields: []string{"security_group", "rule", "preview"}},
	// The names of the fields a manifest of stack_plan has and its type
	// does not: the caller's text, which a manifest a cloned repository
	// brings may write (D108; «Риск», 186, У8).
	"stack_plan":     {fields: []string{"unknown_fields", "error"}},
	"sg_rule_delete": {fields: []string{"security_group", "rule", "still_allowed_by", "still_allowed_from_groups", "vms"}},
}

// markUntrusted adds "_untrusted" to the answers of a tool of
// untrustedFields: the fields of the list the answer has.
func markUntrusted(fields []string, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		text, isErr := next(ctx, args)
		dec := json.NewDecoder(bytes.NewReader([]byte(text)))
		dec.UseNumber()
		var m map[string]any
		if dec.Decode(&m) != nil {
			return text, isErr
		}
		var has []string
		for _, f := range fields {
			if _, ok := m[f]; ok {
				has = append(has, f)
			}
		}
		if len(has) == 0 {
			return text, isErr
		}
		m["_untrusted"] = has
		return toJSON(m), isErr
	}
}
