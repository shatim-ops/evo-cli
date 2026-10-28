// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"regexp"
	"sort"
	"testing"
)

// refArg is an argument that names a resource or an entry of a catalogue:
// an id, or one of the names valueRefs completes, or the key of an
// object. A name of a new resource ("name") is not one.
var refArg = regexp.MustCompile(`(_id|_ids)$|^(region|zone|flavor|image|bucket|path|key|cluster|registry|repository|secret)$`)

// Why a reference is not completed, when it is not.
const (
	// The id is returned by the write that started it (an operation, a
	// job, a plan): there is nothing to list it from.
	refReturned = "returned by the call that made it"
	// Its list takes the argument beside it (a record of a zone, an object
	// of a bucket): Tab has no value of the parent to read it with.
	refParent = "its list needs another argument of the call"
	// No tool lists it.
	refNoList = "no tool lists it"
	// A list without a required argument has it, and nothing declares it:
	// the work item names who declares it.
	refGap = "listed, not declared"
)

// refUncompleted are the references Tab does not complete, with why; for
// refGap, the work item. The list only shrinks: a new reference without a
// list and without a line here fails the build, and so does a line whose
// argument is completed now or is gone (CLAUDE.md §0, rule 3: a new
// surface, Tab, release plan 1.5).
var refUncompleted = map[string]struct{ why, item string }{
	"ar_operation operation_id":                 {why: refReturned},
	"cert_operation operation_id":               {why: refReturned},
	"dns_operation operation_id":                {why: refReturned},
	"kafka_operation operation_id":              {why: refReturned},
	"lb_operation operation_id":                 {why: refReturned},
	"pg_operation operation_id":                 {why: refReturned},
	"redis_operation operation_id":              {why: refReturned},
	"vpc_operation operation_id":                {why: refReturned},
	"stack_apply job_id":                        {why: refReturned},
	"stack_destroy job_id":                      {why: refReturned},
	"stack_status job_id":                       {why: refReturned},
	"deploy_apply plan_id":                      {why: refReturned},
	"deploy_plan base_plan_id":                  {why: refReturned},
	"cert_private_key version_id":               {why: refParent},
	"dns_record_delete record_id":               {why: refParent},
	"dns_record_get record_id":                  {why: refParent},
	"vpc_route_delete route_id":                 {why: refParent},
	"s3_get key":                                {why: refParent},
	"s3_put key":                                {why: refParent},
	"s3_rm key":                                 {why: refParent},
	"secret_create kms_key_id":                  {why: refNoList},
	"tg_create interface_id":                    {why: refNoList},
	"vpc_route_create next_hop_interface_id":    {why: refNoList},
	"vpc_route_create next_hop_magic_router_id": {why: refNoList},
	"vpc_route_create next_hop_vip_id":          {why: refNoList},
}

// Release plan 1.5, Tab in the whole CLI: an argument that references a
// resource or an entry of a catalogue is completed (completionOf: valueRefs,
// the kinds of idArgs), or noCompletion says why not, or refUncompleted
// does. Escaped before the invariant (#478, «разработка»): a reference by
// name in a new tool without a line in valueRefs or noCompletion was left
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
			if _, no := noCompletion[tl.Name][arg]; no {
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
		t.Errorf("%s: a reference Tab does not complete — declare its list (valueRefs, idArgs) or why not (noCompletion, refUncompleted)", k)
	}
	for k, line := range refUncompleted {
		if !seen[k] {
			t.Errorf("refUncompleted names %q, which is no reference of a tool: remove the line", k)
		}
		switch line.why {
		case refReturned, refParent, refNoList, refGap:
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
