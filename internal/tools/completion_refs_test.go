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

// Release plan 1.5, Tab in the whole CLI: an argument that references a
// resource or an entry of a catalogue is completed (completionOf: valueRefs,
// the kinds of Decl.IDArgs), or Decl.NoCompletion says why not
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
			if !r.Completes(tl.Name, arg) {
				missing = append(missing, k)
			}

			seen[k] = true
		}
	}
	sort.Strings(missing)
	for _, k := range missing {
		t.Errorf("%s: a reference Tab does not complete — declare its list (valueRefs, Decl.IDArgs) or why not (Decl.NoCompletion)", k)
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

func TestComplete_ReferencesWithoutServiceCatalogue(t *testing.T) {
	r := New(nil)
	for _, tc := range []struct{ tool, arg, missing, adjacent string }{
		{"secret_create", "kms_key_id", "kms_keys", "secret_list"},
		{"vpc_route_create", "next_hop_magic_router_id", "magic_router_list", "vpc_list"},
		{"vpc_route_create", "next_hop_vip_id", "vip_list", "list_floating_ips"},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			tl, ok := r.Lookup(tc.tool)
			if !ok || tl.Decl.NoCompletion[tc.arg] == "" {
				t.Fatal(tc.tool)
			}
			if _, ok := r.Lookup(tc.missing); ok {
				t.Fatalf("revisit completion: %s now exists", tc.missing)
			}
			if !r.listable(tc.adjacent) {
				t.Fatalf("missing adjacent catalogue %s", tc.adjacent)
			}
			if r.Completes(tc.tool, tc.arg) || len(r.Complete(context.Background(), tc.tool, tc.arg, "")) != 0 {
				t.Fatal("offered a resource of an unrelated catalogue")
			}
			if err := CheckArgs(tl, map[string]any{tc.arg: resMine}); err != nil {
				t.Fatal("explicit ID refused", err)
			}
		})
	}
}

func TestComplete_BucketCertificateAndResourceType(t *testing.T) {
	for _, tc := range []struct{ tool, arg, parent, source, collection, field, value string }{
		{"s3_get", "key", "bucket-one", "s3_ls", "objects", "key", "docs/one"},
		{"s3_put", "key", "bucket-one", "s3_ls", "objects", "key", "docs/one"},
		{"s3_rm", "key", "bucket-one", "s3_ls", "objects", "key", "docs/one"},
		{"cert_private_key", "version_id", resMine, "cert_versions", "versions", "id", "12"},
		{"backup_list", "resource_id", "disk", "disk_list", "disks", "id", zoneMine},
		{"backup_list", "resource_id", "virtual_machine", "vm_list", "vms", "id", resMine},
	} {
		t.Run(tc.tool+"/"+tc.parent, func(t *testing.T) {
			r := New(nil)
			calls := 0
			if got := r.CompleteFor(context.Background(), tc.tool, tc.arg, "", ""); len(got) != 0 {
				t.Fatal(got)
			}
			r.unchecked["cert_list"] = func(ctx context.Context, args map[string]any) (string, bool) {
				return `{"certificates":[{"id":"` + resMine + `"}]}`, false
			}
			r.unchecked[tc.source] = func(_ context.Context, args map[string]any) (string, bool) {
				calls++
				switch tc.source {
				case "s3_ls":
					if args["bucket"] != tc.parent {
						return `{"objects":[]}`, false
					}
				case "cert_versions":
					if args["certificate_id"] != tc.parent {
						t.Fatal(args)
					}
					return `{"versions":[{"id":12}]}`, false
				}
				return toJSON(map[string]any{tc.collection: []any{map[string]any{tc.field: tc.value}}}), false
			}
			if got := r.CompleteFor(context.Background(), tc.tool, tc.arg, "", tc.parent); len(got) != 1 || got[0].Value != tc.value || calls != 1 {
				t.Fatal(got, calls)
			}
			if got := r.CompleteFor(context.Background(), tc.tool, tc.arg, "", resForeign); len(got) != 0 {
				t.Fatal("cross-parent result", got)
			}
			if (tc.source == "cert_versions" || tc.tool == "backup_list") && calls != 1 {
				t.Fatal("lookup for unverified parent", calls)
			}
			r.unchecked[tc.source] = func(context.Context, map[string]any) (string, bool) { return "unavailable", true }
			if got := r.CompleteFor(context.Background(), tc.tool, tc.arg, "", tc.parent); len(got) != 0 {
				t.Fatal("stale suggestions", got)
			}
		})
	}
}

func TestComplete_SecurityGroupAndRouteChildren(t *testing.T) {
	ctx := context.Background()
	sg := &sgCloud{rules: []*provision.SGRule{{ID: zoneMine}}}
	r := completionRegistry(t, sg, completionCfg)
	if got := r.CompleteFor(ctx, "sg_rule_delete", "rule_id", "", "SSH-access_ru.AZ-1"); len(got) != 1 || got[0].Value != zoneMine {
		t.Fatal(got)
	}
	if got := r.CompleteFor(ctx, "sg_rule_delete", "rule_id", "", resForeign); len(got) != 0 || len(sg.deleted) != 0 {
		t.Fatal(got, sg.deleted)
	}
	f := &ownerFake{}
	r = ownerRegistry(t, f, true, ownProject)
	for _, parent := range []string{resMine, "res-a1", resForeign} {
		got := r.CompleteFor(ctx, "vpc_route_delete", "route_id", "", parent)
		if parent == resForeign {
			if len(got) != 0 {
				t.Fatal(parent, got)
			}
		} else if len(got) != 1 || got[0].Value != ownerRoute {
			t.Fatal(parent, got, f.log())
		}
	}
	for _, req := range f.log() {
		if !strings.HasPrefix(req, "GET ") || strings.Contains(req, resForeign+"/routes") {
			t.Fatal(req)
		}
	}
}
