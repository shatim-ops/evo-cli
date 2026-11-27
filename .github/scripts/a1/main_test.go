// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

const pID = "10000000-0000-4000-8000-000000000001"
const gID = "20000000-0000-4000-8000-000000000002"
const rID = "30000000-0000-4000-8000-000000000003"
const zID = "40000000-0000-4000-8000-000000000004"
const other = "50000000-0000-4000-8000-000000000005"

type fake struct {
	t                           *testing.T
	j                           *job
	group, rule, foreign, bound bool
	delayedRead                 bool
	trouble                     string
	gp, rp, rd, gd              int
}

func (f *fake) RoundTrip(r *http.Request) (*http.Response, error) {
	path := r.URL.Path
	card := groups + "/" + gID
	rules := card + "/rules"
	status := 200
	var data any
	switch {
	case r.Method == "GET" && path == "/api/v1/availability-zones":
		data = map[string]any{"items": []map[string]any{{"id": zID, "name": zoneName}}, "total": 1}
	case r.Method == "POST" && path == groups:
		f.gp++
		if f.gp != 1 {
			f.t.Fatal("repeat group POST")
		}
		f.group = true
		if f.trouble == "initial rule" {
			f.foreign = true
		}
		var in map[string]string
		if json.NewDecoder(r.Body).Decode(&in) != nil || in["project_id"] != pID || in["availability_zone_id"] != zID || in["description"] != f.j.name() || in["name"] != f.j.name() {
			f.t.Fatal("bad group target")
		}
		if f.trouble == "group response lost" {
			return nil, io.ErrUnexpectedEOF
		}
		data = map[string]any{"id": gID, "ingress_rules_count": 0, "egress_rules_count": 0}
		if f.trouble == "initial count only" {
			data = map[string]any{"id": gID, "egress_rules_count": 1}
		}
	case r.Method == "GET" && path == card:
		if !f.group {
			status = 404
			data = map[string]string{"message": "Security group not found"}
			if f.trouble == "gateway 404" {
				data = map[string]string{"message": "route not found"}
			}
		} else {
			name := f.j.name()
			if f.trouble == "replaced" && f.rp > 0 {
				name = "other"
			}
			row := map[string]any{"id": gID, "name": name, "description": name, "state": "created", "has_interfaces": f.bound, "project_id": pID, "availability_zone_id": zID, "availability_zone": map[string]string{"id": zID, "name": zoneName}}
			if f.rp > 0 {
				switch f.trouble {
				case "count mismatch":
					row["egress_rules_count"] = 1
				case "default group":
					row["default"] = true
				case "zone mismatch":
					row["availability_zone"] = map[string]string{"id": other}
				case "project mismatch":
					row["project"] = map[string]string{"id": other}
				}
			}
			if f.trouble == "bound once after rule delete" && f.rd > 0 && !f.delayedRead {
				row["has_interfaces"] = true
				f.delayedRead = true
			}
			data = row
		}
	case r.Method == "POST" && path == rules:
		if r.URL.Query().Get("project_id") != pID || !f.group || f.rule || f.foreign {
			f.t.Fatal("POST rule outside empty own group")
		}
		f.rp++
		f.rule = true
		var row provision.SGRule
		if json.NewDecoder(r.Body).Decode(&row) != nil || row != f.j.expectedRule() {
			f.t.Fatal("unexpected created rule")
		}
		if f.trouble == "rule response lost" {
			return nil, io.ErrUnexpectedEOF
		}
		if f.trouble == "bound" {
			f.bound = true
		}
		if f.trouble == "foreign alongside" {
			f.foreign = true
		}
		data = map[string]string{"id": rID}
	case r.Method == "GET" && path == rules:
		if f.trouble == "delayed rule removal" && f.rd > 0 {
			if f.delayedRead {
				f.rule = false
			} else {
				f.delayedRead = true
			}
		}
		rows := []provision.SGRule{}
		if f.rule && !(f.trouble == "refused rule delete" && f.rd > 0) {
			row := f.j.expectedRule()
			row.ID = rID
			row.State = "created"
			if f.trouble == "changed rule" {
				row.Description = "someone else"
			}
			rows = append(rows, row)
		}
		if f.trouble == "foreign only on candidate read" && f.rd > 0 && !f.delayedRead {
			rows = append(rows, provision.SGRule{ID: other})
			f.delayedRead = true
		}
		if f.foreign {
			rows = append(rows, provision.SGRule{ID: other})
		}
		total := len(rows)
		offset := 0
		if f.trouble == "partial list" {
			total++
		}
		if f.trouble == "wrong offset" {
			offset = 1
		}
		page := map[string]any{"items": rows, "offset": offset, "limit": 100, "total": total}
		if f.trouble == "missing total" {
			delete(page, "total")
		}
		if f.trouble == "continued page" {
			page["next_page_token"] = "pending"
		}
		data = page
	case r.Method == "DELETE" && path == rules+"/"+rID:
		if !f.rule || f.foreign || f.bound || !f.group || f.rd > 0 {
			f.t.Fatal("unsafe or repeated rule DELETE")
		}
		f.rd++
		if f.trouble == "refused rule delete" {
			status = 403
			break
		}
		f.rule = f.trouble == "delayed rule removal"
		status = 204
		if f.trouble == "foreign before group delete" {
			f.foreign = true
		}
		if f.trouble == "lost DELETE" {
			return nil, io.ErrUnexpectedEOF
		}
	case r.Method == "DELETE" && path == card:
		if !f.group || f.rule || f.foreign || f.bound || f.gd > 0 {
			f.t.Fatal("unsafe or repeated group DELETE")
		}
		f.gd++
		f.group = false
		if f.trouble == "refused group delete" {
			status = 403
			break
		}
		status = 204
		if f.trouble == "lost group DELETE" {
			return nil, io.ErrUnexpectedEOF
		}
	default:
		f.t.Fatalf("unexpected request %s %s", r.Method, path)
	}
	b, err := json.Marshal(data)
	if err != nil {
		f.t.Fatal(err)
	}
	header := http.Header{"Content-Type": {"application/json"}}
	if path == rules && r.Method == "GET" && f.trouble == "range header" {
		header.Set("Content-Range", "items 5-5/6")
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Header: header, Body: io.NopCloser(bytes.NewReader(b)), Request: r}, nil
}

func fixture(t *testing.T, trouble string) (*job, *fake) {
	t.Helper()
	j := &job{r: receipt{Run: "317", Project: pID}, dir: t.TempDir(), key: "00000000000000000000000000000001", bearer: "fixture", pause: time.Millisecond}
	f := &fake{t: t, j: j, trouble: trouble}
	j.raw = &http.Client{Transport: f, CheckRedirect: noRedirect}
	return j, f
}

func reopen(t *testing.T, j *job) *job {
	t.Helper()
	k := *j
	k.r = receipt{Run: j.r.Run, Project: j.r.Project, Principal: j.r.Principal}
	b, err := os.ReadFile(k.path())
	if err != nil || k.loadReceipt(b) != nil {
		t.Fatal("missing receipt", err)
	}
	return &k
}

func TestCandidateDestroyThenCleanupOwnGroup(t *testing.T) {
	for _, trouble := range []string{"", "delayed rule removal"} {
		t.Run(trouble, func(t *testing.T) {
			j, f := fixture(t, trouble)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			err := j.probe(ctx)
			if err != nil {
				t.Fatal("candidate did not close its rule", err)
			}
			if f.gp != 1 || f.rp != 1 || f.rd != 1 || f.gd != 0 {
				t.Fatalf("unexpected mutations after candidate: %d,%d,%d,%d", f.gp, f.rp, f.rd, f.gd)
			}
			k := reopen(t, j)
			if err := k.cleanup(ctx); err != nil {
				t.Fatal("cleanup failed", err)
			}
			if !k.r.GroupGone || !k.r.RuleGone || f.group || f.rule || f.gd != 1 {
				t.Fatalf("cleanup not confirmed: %+v", k.r)
			}
			if !k.r.CandidateConfirmed {
				t.Fatal("ordinary candidate result not confirmed")
			}
			if err := reopen(t, k).cleanup(ctx); err != nil || f.gd != 1 || f.rd != 1 {
				t.Fatal("retry repeated a mutation")
			}
		})
	}
}

func TestUncertainOwnershipNeverWidensCleanup(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		rulePOST, ruleDELETE, groupDELETE int
	}{
		{"initial count only", 0, 0, 0}, {"missing total", 0, 0, 0}, {"range header", 0, 0, 0}, {"count mismatch", 1, 0, 0},
		{"group response lost", 0, 0, 0}, {"initial rule", 0, 0, 0}, {"rule response lost", 1, 0, 0}, {"partial list", 0, 0, 0}, {"wrong offset", 0, 0, 0}, {"continued page", 0, 0, 0},
		{"foreign alongside", 1, 0, 0}, {"foreign before group delete", 1, 1, 0}, {"changed rule", 1, 0, 0},
		{"default group", 1, 0, 0}, {"zone mismatch", 1, 0, 0}, {"project mismatch", 1, 0, 0},
		{"bound", 1, 0, 0}, {"replaced", 1, 0, 0}, {"gateway 404", 1, 1, 1}, {"lost group DELETE", 1, 1, 1}, {"lost DELETE", 1, 1, 0}, {"refused rule delete", 1, 1, 0}, {"refused group delete", 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, f := fixture(t, tc.name)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_ = j.probe(ctx)
			k := reopen(t, j)
			if err := k.cleanup(ctx); err == nil || k.r.GroupGone {
				t.Fatal("cleanup asserted certainty")
			}
			if f.gp != 1 || f.rp != tc.rulePOST || f.rd != tc.ruleDELETE || f.gd != tc.groupDELETE {
				t.Fatalf("unexpected mutations group POST=%d, rule POST=%d, rule DELETE=%d, group DELETE=%d", f.gp, f.rp, f.rd, f.gd)
			}
		})
	}
}

func TestForeignRuleRemainsAStopAfterDisappearing(t *testing.T) {
	for _, trouble := range []string{"initial rule", "foreign alongside"} {
		t.Run(trouble, func(t *testing.T) {
			j, f := fixture(t, trouble)
			if j.probe(t.Context()) == nil {
				t.Fatal("probe did not stop")
			}
			f.foreign = false
			if reopen(t, j).cleanup(t.Context()) == nil || f.gd != 0 || f.rd != 0 {
				t.Fatal("scope was widened on retry")
			}
		})
	}
}

func TestRescueStopsIfOwnershipChangesDuringCandidateRead(t *testing.T) {
	for _, trouble := range []string{"bound once after rule delete", "foreign only on candidate read"} {
		t.Run(trouble, func(t *testing.T) {
			j, f := fixture(t, trouble)
			f.group, f.rule, f.gp, f.rp = true, true, 1, 1
			j.r.ZoneID, j.r.Group, j.r.Rule = zID, gID, rID
			j.r.GroupPOST, j.r.RulePOST, j.r.InitialEmpty = true, true, true
			if err := j.save(); err != nil {
				t.Fatal(err)
			}
			if err := j.cleanup(t.Context()); err == nil || !f.delayedRead || !j.r.ScopeMismatch || f.gd != 0 || f.rd != 1 {
				t.Fatal("rescue ignored changed scope")
			}
			if err := reopen(t, j).cleanup(t.Context()); err == nil || f.gd != 0 {
				t.Fatal("resumed despite ownership uncertainty")
			}
		})
	}
}

func TestReceiptRejectsMissingOrInconsistentEvidence(t *testing.T) {
	j, f := fixture(t, "")
	if err := j.probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(j.path())
	if err != nil {
		t.Fatal(err)
	}
	if err = j.loadReceipt(raw); err != nil {
		t.Fatal(err)
	}
	if f.gd != 0 {
		t.Fatal("unexpected group delete")
	}
	bad := j.r
	bad.GroupGone = true
	impossible, _ := json.Marshal(bad)
	for _, b := range [][]byte{[]byte("{}"), []byte("null"), []byte(`{"GroupPOST":false}`), impossible} {
		if err := j.loadReceipt(b); err == nil {
			t.Fatal("incomplete or inconsistent receipt accepted")
		}
	}
	// An interruption after persisting the intent but before receiving the
	// DELETE response cannot be accepted as proof of deletion on a retry.
	j.r.GroupDELETE = true
	if err := j.save(); err != nil {
		t.Fatal(err)
	}
	f.group = false
	if err := reopen(t, j).cleanup(t.Context()); err == nil || f.gd != 0 {
		t.Fatal("unacknowledged delete accepted or retried")
	}
}

func TestFenceRejectsOtherPaths(t *testing.T) {
	j, f := fixture(t, "")
	j.r.Group, j.r.Rule = gID, rID
	for _, p := range []string{"/api/v1/vms/" + other, "/api/v1/security-groups/" + other, groups + "/" + gID + "/rules/" + other, groups + "/" + gID + "?recursive=true"} {
		r, err := http.NewRequest(http.MethodDelete, compute+p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = (fence{j}).RoundTrip(r); err == nil {
			t.Fatalf("accepted out-of-receipt path %s", p)
		}
	}
	if f.rd != 0 || f.gd != 0 {
		t.Fatal("out-of-receipt mutation")
	}
	for _, b := range []string{"not found", `[]`, `{"message":"not found"}`, `{"message":"security group route not found"}`, `{"message":"security group not found","code":"gateway"}`, `[{"message":"security group not found"},{"message":"security group not found"}]`} {
		if groupAbsent([]byte(b)) {
			t.Fatal("ambiguous 404 accepted")
		}
	}
	if !groupAbsent([]byte(`[{"code":"security_group_not_found","message":"Security group with this id is not found"}]`)) {
		t.Fatal("resource 404 refused")
	}
	if id(strings.Repeat("0", 8) + "-0000-0000-0000-000000000000") {
		t.Fatal("nil UUID accepted")
	}
}
