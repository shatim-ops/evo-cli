// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// The answer of a change says whether the change is over (CLAUDE.md §6,
// axis 2; owner's decision 11.11, Н3; D99, П5). The status of a call with
// consent is one of two vocabularies, and a word of neither fails: a new
// tool says which its statuses are.

// statusGoingOn are the statuses of a change the cloud is still making:
// the call has answered, the resource is not there yet (or not gone yet).
var statusGoingOn = map[string]bool{
	"backing_up": true, "building": true, "creating": true, "deleting": true,
	"deploying": true, "installing": true, "issuing": true, "renewing": true,
	// vm_start (D99): the VM is on its way; and the state of another
	// change has not settled by the limit of the call.
	"starting": true, "waiting": true,
	// backup_restore: the platform took the restore, the disk is on its
	// way or not listed yet.
	"restoring": true,
	// vm_stop (D99, §4).
	"stopping": true,
	// k8s_nodepool_scale (D99): the pool is on its way to the count.
	"scaling": true,
	// vm_reboot (D99, §5).
	"rebooting": true,
}

// statusOver are the statuses of a change that is over, as far as the
// call goes: made, or found made already (already_<outcome>).
var statusOver = map[string]bool{
	"applied": true, "attached": true, "created": true, "deleted": true,
	"destroyed": true, "detached": true, "done": true, "imported": true,
	"none": true, "ok": true, "resized": true, "running": true,
	"version_added": true,
	"started":       true,
	"stopped":       true,
	"scaled":        true,
	// vm_reboot (D99, §5): the reboot seen to its end; and a command the
	// platform took whose transition evo did not see — over as far as the
	// call goes, with the read of the VM as the step.
	"rebooted": true, "reboot_accepted": true,
}

// noNextStep are the tools shipped before the invariant whose answer says
// the change is going on and names no next step for the machine: the agent
// polls a list blindly (R-17). "tool" → what the answer has. It only
// shrinks, and a new tool does not join it. Closes by 1.10 (release plan
// 1.9, «Сроки храповиков «к 1.7–1.9»», #1590 question 19 (c)); release plan
// 1.9, «Инварианты до новых изменяющих операций», Н3; R-17: next_tool and
// next_args are additive, not breaking).
var noNextStep = map[string]string{
	"ar_create":         "creating, operation_id: next is ar_operation",
	"ca_deploy":         "deploying, operation_id",
	"cert_issue_le":     "issuing, no operation_id",
	"cert_renew":        "renewing, no operation_id",
	"dns_record_create": "creating, operation_id: next is dns_operation",
	"dns_zone_create":   "creating, operation_id: next is dns_operation",
	"image_create":      "building, no operation_id",
	"k8s_add_addon":     "installing, operation_id: next is k8s_operation",
	"k8s_add_nodepool":  "creating, operation_id: next is k8s_operation",
	"k8s_create":        "creating, operation_id: next is k8s_operation",
	"kafka_create":      "creating, operation_id: next is kafka_operation",
	"lb_create":         "creating, operation_id: next is lb_operation",
	"pg_backup_create":  "backing_up, operation_id: next is pg_operation",
	"pg_create":         "creating, operation_id: next is pg_operation",
	"redis_create":      "creating, operation_id: next is redis_operation",
	"tg_create":         "creating, operation_id: next is lb_operation",
	"vpc_create":        "creating, operation_id: next is vpc_operation",
	"vpc_route_create":  "creating, operation_id: next is vpc_operation",
	"vpc_route_delete":  "deleting, operation_id: next is vpc_operation",
}

// nextStepProblem reads the answer of a change that is going on: "" when
// it names the machine's next step (D69, nextStep), else what is wrong.
// One rule for the two ways the platform reports a change: with an
// operation — the next step is the service's *_operation with the id of
// the answer; without one (a VM's power: 204 and no id, D99) — the tool
// itself, whose repeat reads the state and waits without a write, or a
// tool that only reads.
func nextStepProblem(tool string, answer map[string]any, registry map[string]bool) string {
	next, _ := answer["next_tool"].(string)
	args, _ := answer["next_args"].(map[string]any)
	switch {
	case next == "":
		return "names no next_tool: the agent has nothing to call but a list"
	case len(args) == 0:
		return "names next_tool " + next + " without next_args"
	}
	mutating, known := registry[next]
	if !known {
		return "names next_tool " + next + ", which is not a tool of the registry"
	}
	if op, _ := answer["operation_id"].(string); op != "" {
		if !strings.HasSuffix(next, "_operation") {
			return fmt.Sprintf("has operation_id, and its next_tool is %s, not the read of the operation (*_operation)", next)
		}
		if args["operation_id"] != op {
			return fmt.Sprintf("next_args.operation_id is %v, the answer's operation_id is %s", args["operation_id"], op)
		}
		return ""
	}
	if mutating && next != tool {
		return "names next_tool " + next + ", another tool that changes the cloud: the next step of a change going on is the same call or a read"
	}
	return ""
}

// TestProgress_ChangeGoingOnNamesTheNextStep: every command and tool with a
// case of parity, called with consent in the JSON world. A status of
// neither vocabulary fails; an answer whose status says the change is
// going on names the next step.
func TestProgress_ChangeGoingOnNamesTheNextStep(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	registry := map[string]bool{}
	for _, tl := range newRegistry().Tools() {
		registry[tl.Name] = tl.Mutating
	}
	found := map[string]string{}
	goingOn := 0
	for _, c := range cases {
		if !c.tool.Mutating {
			continue
		}
		r := runJSONCase(t, c, w, fake)
		if r.isErr {
			continue
		}
		var answer map[string]any
		if err := json.Unmarshal([]byte(r.text), &answer); err != nil {
			continue
		}
		status, _ := answer["status"].(string)
		switch {
		case statusOver[status] || strings.HasPrefix(status, "already_"):
		case statusGoingOn[status]:
			goingOn++
			if problem := nextStepProblem(c.tool.Name, answer, registry); problem != "" {
				found[c.tool.Name] = fmt.Sprintf("status %s, %s", status, problem)
			}
		default:
			t.Errorf("%s answers status %q, a word of neither vocabulary: add it to statusGoingOn (the cloud is still making the change — the answer then names next_tool) or to statusOver (the change is made)", c.tool.Name, status)
		}
	}
	if goingOn == 0 {
		t.Fatal("no answer of the JSON world says a change is going on: the check sees nothing")
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if noNextStep[name] == "" {
			t.Errorf("%s: %s — put next_tool and next_args into the answer (nextStep): the read of the operation when the platform gives its id, else the same call, whose repeat reads the state and does not write (D99, П5)", name, found[name])
		}
	}
	for name := range noNextStep {
		if found[name] == "" {
			t.Errorf("noNextStep names %s, whose answer names the next step now or is no longer going on: remove the line (the list only shrinks)", name)
		}
	}
}
