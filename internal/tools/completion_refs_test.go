// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// refArg is an argument that names a resource or an entry of a catalogue:
// an id (_id or _ids at the end or before another word: none in the
// middle today, Qwen on #483), or one of the names valueRefs completes, or
// the key of an object. A name of a new resource ("name") is not one.
var refArg = regexp.MustCompile(`_ids?(_|$)|^(region|zone|flavor|image|bucket|path|key|cluster|registry|repository|secret)$`)

// Why a reference is not completed, when it is not.
const (
	// Its list takes the argument beside it (a record of a zone, an object
	// of a bucket): Tab has no value of the parent to read it with.
	refParent = "its list needs another argument of the call"
	// No tool lists it.
	refNoList = "no tool lists it"
	// A VM or a disk: two lists, and Tab has no kind to read one of them by.
	refTwoKinds = "a VM or a disk, which no one list holds"
	// A list without a required argument has it, and nothing declares it:
	// the work item names who declares it.
	refGap = "listed, not declared"
)

// refUncompleted are the references Tab does not complete, with why; for
// refGap, the work item. The list only shrinks: a new reference without a
// list and without a line here fails the build, and so does a line whose
// argument is completed now or is gone (CLAUDE.md §0, rule 3: a new
// surface, Tab, release plan 1.5).
// Closes by 1.10: moved from 1.8 by the owner's decision of 13.11 (#1590,
// question 19; release plan 1.9, «Просроченные убывающие списки»).
var refUncompleted = map[string]struct{ why, item string }{

	"sg_rule_delete rule_id": {why: refParent},

	"cert_private_key version_id": {why: refParent},

	"vpc_route_delete route_id": {why: refParent},
	"s3_get key":                {why: refParent},
	"s3_put key":                {why: refParent},
	"s3_rm key":                 {why: refParent},
	"secret_create kms_key_id":  {why: refNoList},
	"backup_list resource_id":   {why: refTwoKinds},

	"vpc_route_create next_hop_magic_router_id": {why: refNoList},
	"vpc_route_create next_hop_vip_id":          {why: refNoList},
}

// Release plan 1.5, Tab in the whole CLI: an argument that references a
// resource or an entry of a catalogue is completed (completionOf: valueRefs,
// the kinds of Decl.IDArgs), or Decl.NoCompletion says why not, or refUncompleted
// does. Escaped before the invariant (#478, «разработка»): a reference by
// name in a new tool without a line in valueRefs or Decl.NoCompletion was left
// without Tab, silently.
func TestCompletion_EveryReferenceIsCompletedOrSaysWhy(t *testing.T) {
	r := New(nil)
	seen := map[string]bool{}
	var missing []string
	for _, tl := range r.Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for arg := range props {
			if !refArg.MatchString(arg) {
				continue
			}
			if _, no := tl.Decl.NoCompletion[arg]; no {
				continue
			}
			k := tl.Name + " " + arg
			line, declared := refUncompleted[k]
			switch completes := r.Completes(tl.Name, arg); {
			case completes && declared:
				t.Errorf("refUncompleted names %q, which Tab completes now: remove the line", k)
			case !completes && !declared:
				missing = append(missing, k)
			case declared && line.why == refGap && line.item == "":
				t.Errorf("%q: a gap without its work item", k)
			}
			seen[k] = true
		}
	}
	sort.Strings(missing)
	for _, k := range missing {
		t.Errorf("%s: a reference Tab does not complete — declare its list (valueRefs, Decl.IDArgs) or why not (Decl.NoCompletion, refUncompleted)", k)
	}
	for k, line := range refUncompleted {
		if !seen[k] {
			t.Errorf("refUncompleted names %q, which is no reference of a tool: remove the line", k)
		}
		switch line.why {
		case refParent, refNoList, refTwoKinds, refGap:
		default:
			t.Errorf("refUncompleted[%q]: unknown reason %q", k, line.why)
		}
		if line.why != refGap && line.item != "" {
			t.Errorf("refUncompleted[%q]: an item belongs to a gap only", k)
		}
	}
	if !seen["vm_get vm_id"] {
		t.Fatal("vm_get vm_id is not among the references: refArg moved, update the test")
	}
}

// Tab reads, it does not call: no record in the journal (D46) — a person
// pressing Tab ten times would otherwise fill it with lists they never
// ran (completion.go: «Nothing here … goes to the journal»).
func TestComplete_WritesNoJournal(t *testing.T) {
	cloud := &zonesCloud{cancelCloud: cancelCloud{cancel: func() {}}}
	r := completionRegistry(t, cloud, completionCfg)
	journalOn(t)
	if got := r.Complete(context.Background(), "provision", "region", ""); len(got) == 0 {
		t.Fatal("Tab offered nothing: the test proves nothing")
	}
	if recs := journalRecords(t); len(recs) != 0 {
		t.Errorf("Tab wrote %d journal records: %+v", len(recs), recs)
	}
	// The journal is on: a call through the handler is recorded.
	tl, _ := r.Lookup("list_zones")
	tl.Handler(context.Background(), map[string]any{})
	if recs := journalRecords(t); len(recs) != 1 {
		t.Errorf("a call of list_zones: %d records, want 1 — the journal is not on in this test", len(recs))
	}
}

// refArg takes an id wherever _id ends a word of the name, and nothing
// that only begins with "id" (Qwen on #494: the wider match is the point).
func TestRefArg_IDsAnywhereInTheName(t *testing.T) {
	for arg, want := range map[string]bool{
		"vm_id": true, "subnet_ids": true, "next_hop_vip_id": true,
		"source_id_list": true, "vm_ids_extra": true,
		"zone": true, "name": false, "provider_identity": false, "idle_timeout": false, "vm_idx": false,
	} {
		if got := refArg.MatchString(arg); got != want {
			t.Errorf("refArg(%q) = %v, want %v", arg, got, want)
		}
	}
}

func TestComplete_ReturnedIDsUseTheirReceipt(t *testing.T) {
	r := New(nil) // any attempt to build a client or read a catalogue fails.
	for _, tc := range []struct {
		tool, arg string
		kind      *ownerKind
		why       string
	}{
		{"ar_operation", "operation_id", kindOperation, whyReceiptOperation},
		{"cert_operation", "operation_id", kindOperation, whyReceiptOperation},
		{"dns_operation", "operation_id", kindOperation, whyReceiptOperation},
		{"k8s_operation", "operation_id", kindOperation, whyReceiptOperation},
		{"kafka_operation", "operation_id", kindOperation, whyReceiptOperation},
		{"lb_operation", "operation_id", kindOperation, whyReceiptOperation},
		{"pg_operation", "operation_id", kindOperation, whyReceiptOperation},
		{"redis_operation", "operation_id", kindOperation, whyReceiptOperation},
		{"vpc_operation", "operation_id", kindOperation, whyReceiptOperation},
		{"stack_apply", "job_id", nil, whyReceiptJob},
		{"stack_destroy", "job_id", nil, whyReceiptJob},
		{"stack_status", "job_id", nil, whyReceiptJob},
		{"deploy_apply", "plan_id", nil, whyReceiptPlan},
		{"deploy_plan", "base_plan_id", nil, whyReceiptPlan},
	} {
		t.Run(tc.tool+"/"+tc.arg, func(t *testing.T) {
			tl, ok := r.Lookup(tc.tool)
			if !ok || tl.Decl.NoCompletion[tc.arg] != tc.why {
				t.Fatalf("missing receipt source for %s", tc.arg)
			}
			found := false
			for _, a := range tl.Decl.IDArgs {
				if a.name == tc.arg && a.kind == tc.kind && (tc.kind != nil || a.why == whyLocal) {
					found = true
				}
			}
			if !found {
				t.Fatal("expected an operation receipt or a local stored plan/job")
			}
			if r.Completes(tc.tool, tc.arg) || len(r.Complete(context.Background(), tc.tool, tc.arg, "")) != 0 {
				t.Fatal("offered an unrelated ID")
			}
		})
	}
}

// These reads enumerate children only under a caller-selected parent. The
// child commands have no parent argument; Tab must not pick one arbitrarily.
func TestComplete_ChildWithoutParentInput(t *testing.T) {
	r := New(nil)
	for _, tc := range []struct {
		tools               []string
		arg, source, parent string
	}{
		{[]string{"k8s_nodepool_get", "k8s_nodepool_delete", "k8s_nodepool_scale"}, "nodepool_id", "k8s_nodepools", "cluster_id"},
		{[]string{"dns_record_get", "dns_record_delete"}, "record_id", "dns_records", "zone_id"},
	} {
		src, ok := r.Lookup(tc.source)
		req, _ := src.InputSchema["required"].([]string)
		if !ok || src.Mutating || !slices.Contains(req, tc.parent) {
			t.Fatal(tc.source, req)
		}
		for _, tool := range tc.tools {
			target, _ := r.Lookup(tool)
			props := target.InputSchema["properties"].(map[string]any)
			reason := target.Decl.NoCompletion[tc.arg]
			if props[tc.parent] != nil || !strings.Contains(reason, tc.source) || !strings.Contains(reason, tc.parent) {
				t.Fatal(tool, reason)
			}
			if r.Completes(tool, tc.arg) || len(r.Complete(context.Background(), tool, tc.arg, "")) != 0 {
				t.Fatal("Tab picked a parent for", tool)
			}
		}
	}
}

type tabInterfacesCloud struct {
	cancelCloud
	projects []string
	foreign  bool
}

func (c *tabInterfacesCloud) ListInterfaces(_ context.Context, p string) ([]*provision.Interface, error) {
	c.projects = append(c.projects, p)
	rows := []*provision.Interface{{ID: zoneMine, ProjectID: p, VM: &provision.InterfaceVM{ID: resMine}}, {ID: zoneOther, ProjectID: p, VM: &provision.InterfaceVM{ID: resOther}}, {ID: zoneAZ, ProjectID: p}}
	if c.foreign {
		rows[0].ProjectID = ownForeign
	}
	return rows, nil
}

func TestComplete_InterfaceRequiresSelectedVM(t *testing.T) {
	for _, tc := range []struct{ tool, arg, parent string }{
		{"tg_create", "interface_id", "vm_id"}, {"vpc_route_create", "next_hop_interface_id", "next_hop_vm_id"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			c := &tabInterfacesCloud{}
			r := completionRegistry(t, c, completionCfg)
			if CompletionParent(tc.tool, tc.arg) != tc.parent || !r.Completes(tc.tool, tc.arg) {
				t.Fatal("parent source not registered")
			}
			for _, value := range []string{"", "not-a-uuid"} {
				if got := r.CompleteFor(context.Background(), tc.tool, tc.arg, "", value); len(got) != 0 {
					t.Fatal(got)
				}
			}
			if len(c.projects) != 0 {
				t.Fatal(c.projects)
			}
			for vm, nic := range map[string]string{resMine: zoneMine, resOther: zoneOther} {
				got := r.CompleteFor(context.Background(), tc.tool, tc.arg, "", vm)
				if len(got) != 1 || got[0].Value != nic {
					t.Fatal(vm, got)
				}
			}
			c.foreign = true
			if got := r.CompleteFor(context.Background(), tc.tool, tc.arg, "", resMine); len(got) != 0 {
				t.Fatal(got)
			}
			if !slices.Equal(c.projects, []string{completionCfg.ProjectID, completionCfg.ProjectID, completionCfg.ProjectID}) {
				t.Fatal(c.projects)
			}
		})
	}
}
