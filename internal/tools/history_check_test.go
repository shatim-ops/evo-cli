// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// checkAt fixes the clock of history with check at noon of 17.11.
func checkAt(t *testing.T) {
	t.Helper()
	old := clock.Now
	clock.Now = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { clock.Now = old })
}

// history with check reads by id each live line it can (readByID, the rule
// of doctor) and says what it found; it writes nothing, the journal
// included («Риск», conclusion 315, У4). Only «not found», answered to
// the key that sent the call, puts a line in would_close — said as «this
// key does not see it now», not «gone» (У3); any other answer, and a line
// it does not read — no id from the cloud, no project, in a parent, too
// new, superseded by a later line, another key or none, a kind with no
// read by id — keeps check_result with why (У1, У2, У5). The cloud is only
// read.
func TestHistory_CheckSaysWhatAReadByIDDoesNotSee(t *testing.T) {
	checkAt(t)
	this := &state.Maker{KeyID: config.MaskKeyID("k")} // the key of the fake's engine
	other := &state.Maker{ServiceAccountID: "sa-other", KeyID: "ot…er"}
	old := "2026-11-17T10:00:00Z"
	for _, c := range []struct {
		name, tool, id, outcome, project, parent, at string
		key                                          *state.Maker
		status                                       int
		says                                         string
		reads                                        int
	}{
		{name: "not seen, the same key", tool: "pg_create", id: resGone, outcome: history.Created, key: this, says: checkNotSeen, reads: 1},
		{name: "found by a repeat, not seen", tool: "lb_create", id: resGone, outcome: history.Found, key: this, says: checkNotSeen, reads: 1},
		{name: "partial destroy may still own addresses and rules", tool: "destroy", id: resGone, outcome: history.Unknown, key: this, says: "cleanup_unconfirmed"},
		{name: "missing VM from provision", tool: "provision", id: resGone, outcome: history.Unknown, key: this, says: checkNotSeen, reads: 1},
		{name: "same account different key", tool: "pg_create", id: resGone, outcome: history.Created, key: &state.Maker{KeyID: "different"}, says: checkOtherKey},
		{name: "still there", tool: "pg_create", id: resMine, outcome: history.Created, key: this, says: checkThere, reads: 1},
		{name: "the read is refused", tool: "lb_create", id: resGone, outcome: history.Created, key: this, status: http.StatusForbidden, says: checkReadFailed, reads: 1},
		{name: "the read is throttled", tool: "vpc_create", id: resGone, outcome: history.Created, key: this, status: http.StatusTooManyRequests, says: checkReadFailed, reads: 1},
		{name: "the API fails", tool: "k8s_create", id: resGone, outcome: history.Created, key: this, status: http.StatusBadGateway, says: checkReadFailed, reads: 1},
		{name: "another key", tool: "cert_import", id: resGone, outcome: history.Created, key: other, says: checkOtherKey},
		{name: "card of another project", tool: "pg_create", id: resForeign, outcome: history.Created, key: this, says: checkReadFailed, reads: 1},
		{name: "a line with no key", tool: "pg_create", id: resGone, outcome: history.Created, says: checkNoKey},
		{name: "a kind with no read by id", tool: "disk_create", id: resGone, outcome: history.Created, key: this, says: checkNoRead},
		{name: "an outcome not known: the id may be a name", tool: "pg_create", id: resGone, outcome: history.Unknown, key: this, says: checkNotByID},
		{name: "a line with no project", tool: "pg_create", id: resGone, outcome: history.Created, project: "-", key: this, says: checkNoProject},
		{name: "a resource in a parent", tool: "k8s_add_nodepool", id: resGone, outcome: history.Created, parent: "cluster_id=" + resForeign, key: this, says: checkInParent},
		{name: "made just now", tool: "pg_create", id: resGone, outcome: history.Created, at: "2026-11-17T11:55:00Z", key: this, says: checkTooNew},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &ownerFake{goneStatus: c.status}
			r := ownerRegistry(t, f, false, ownProject)
			eng, _ := r.newEngine(nil)
			hdir := history.DirOf(intent.Dir(eng.StateDir))
			project, at := ownProject, old
			if c.project == "-" {
				project = ""
			}
			if c.at != "" {
				at = c.at
			}
			line := history.Line{Time: at, Tool: c.tool, ProjectID: project, Kind: resourceKind(c.tool), Parent: c.parent,
				ResourceID: c.id, Outcome: c.outcome, IntentID: "a000000000000001", Key: c.key}
			if err := history.Append(hdir, line); err != nil {
				t.Fatal(err)
			}
			before, _ := history.ReadAll(hdir, nil)
			text, isErr := r.handlers["history"](context.Background(), map[string]any{"check": true, "since": "2026-11-01"})
			if isErr {
				t.Fatal(text)
			}
			var a struct {
				Live       []historyLine `json:"live"`
				WouldClose []historyLine `json:"would_close"`
				Checked    struct {
					Read       int `json:"read"`
					WouldClose int `json:"would_close"`
				} `json:"checked"`
			}
			if err := json.Unmarshal([]byte(text), &a); err != nil {
				t.Fatal(err)
			}
			if len(a.Live) != 1 || a.Live[0].CheckResult != c.says || a.Checked.Read != c.reads {
				t.Errorf("live %+v, %d reads; want the line live with check_result %q, %d reads\n%s", a.Live, a.Checked.Read, c.says, c.reads, text)
			}
			if wants := c.says == checkNotSeen; (len(a.WouldClose) == 1) != wants || (a.Checked.WouldClose == 1) != wants {
				t.Errorf("would_close %+v; want it %v\n%s", a.WouldClose, wants, text)
			}
			if after, _ := history.ReadAll(hdir, nil); len(after.Lines) != len(before.Lines) {
				t.Errorf("history with check wrote to the journal: %d lines, were %d", len(after.Lines), len(before.Lines))
			}
			for _, q := range f.log() {
				if !strings.HasPrefix(q, "GET ") {
					t.Errorf("history with check sent %s", q)
				}
			}
		})
	}
}

// A resource of two live lines — made, then found by a repeat — is read
// once, by its later line; the earlier says later_line (У2). The reads stop
// at historyCheckMax; only lines that passed every local condition count,
// and the rest are over_budget.
func TestHistory_CheckReadsTheLaterLineAndKeepsItsBudget(t *testing.T) {
	checkAt(t)
	f := &ownerFake{}
	r := ownerRegistry(t, f, false, ownProject)
	eng, _ := r.newEngine(nil)
	hdir := history.DirOf(intent.Dir(eng.StateDir))
	key := &state.Maker{KeyID: config.MaskKeyID("k")}
	add := func(intentID, tool, outcome, id string) {
		t.Helper()
		l := history.Line{Time: "2026-11-17T09:00:00Z", Tool: tool, ProjectID: ownProject, Kind: resourceKind(tool),
			ResourceID: id, Outcome: outcome, IntentID: intentID, Key: key}
		if err := history.Append(hdir, l); err != nil {
			t.Fatal(err)
		}
	}
	add("c000000000000001", "pg_create", history.Created, resMine)
	add("c000000000000002", "pg_create", history.Found, resMine)
	// Lines it does not read spend no budget: a kind with no read by id.
	for i := 0; i < 3; i++ {
		add(fmt.Sprintf("d00000000000000%d", i), "disk_create", history.Created, fmt.Sprintf("00000000-0000-4000-8000-0000000d000%d", i))
	}
	for i := 0; i < historyCheckMax+2; i++ {
		add(fmt.Sprintf("e0000000000000%02x", i), "pg_create", history.Created, fmt.Sprintf("00000000-0000-4000-8000-0000000e00%02x", i))
	}
	text, _ := r.handlers["history"](context.Background(), map[string]any{"check": true, "since": "2026-11-01"})
	var a struct {
		Live    []historyLine `json:"live"`
		Checked struct {
			Read       int            `json:"read"`
			NotChecked map[string]int `json:"not_checked"`
		} `json:"checked"`
	}
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		t.Fatal(err)
	}
	if a.Live[0].CheckResult != checkLater || a.Live[1].CheckResult != checkThere {
		t.Errorf("the two lines of one resource: %q, %q; want later_line, then there", a.Live[0].CheckResult, a.Live[1].CheckResult)
	}
	if a.Checked.Read != historyCheckMax || a.Checked.NotChecked[checkOverBudget] != 3 || a.Checked.NotChecked[checkNoRead] != 3 {
		t.Errorf("checked %+v; want %d reads, 3 over the budget, 3 with no read by id", a.Checked, historyCheckMax)
	}
}

// Without check history reads no cloud: the journal only.
func TestHistory_WithoutCheckReadsNoCloud(t *testing.T) {
	f := &ownerFake{}
	r := ownerRegistry(t, f, false, ownProject)
	eng, _ := r.newEngine(nil)
	line := history.Line{Time: "2026-11-17T10:00:00Z", Tool: "pg_create", ProjectID: ownProject, Kind: resourceKind("pg_create"),
		ResourceID: resGone, Outcome: history.Created, IntentID: "a000000000000001", Key: &state.Maker{KeyID: config.MaskKeyID("k")}}
	if err := history.Append(history.DirOf(intent.Dir(eng.StateDir)), line); err != nil {
		t.Fatal(err)
	}
	if text, isErr := r.handlers["history"](context.Background(), map[string]any{"since": "2026-11-01"}); isErr || strings.Contains(text, "check_result") {
		t.Errorf("history without check: %s", text)
	}
	if q := f.log(); len(q) != 0 {
		t.Errorf("history without check read the cloud: %v", q)
	}
}

// On a server bound to projects (--allow-project) check reads only the
// lines it shows: a line of another project, or with no project, is not
// read and not closed.
func TestHistory_CheckOnABoundServerReadsOnlyWhatItShows(t *testing.T) {
	checkAt(t)
	f := &ownerFake{}
	r := ownerRegistry(t, f, true, ownProject)
	eng, err := r.newEngine(map[string]any{"ProjectID": ownProject})
	if err != nil {
		t.Fatal(err)
	}
	hdir := history.DirOf(intent.Dir(eng.StateDir))
	key := &state.Maker{KeyID: config.MaskKeyID("k")}
	for i, project := range []string{"00000000-0000-4000-8000-0000000000bb", ""} {
		l := history.Line{Time: "2026-11-17T10:00:00Z", Tool: "pg_create", ProjectID: project, Kind: resourceKind("pg_create"),
			ResourceID: resGone, Outcome: history.Created, IntentID: "b00000000000000" + string(rune('1'+i)), Key: key}
		if err := history.Append(hdir, l); err != nil {
			t.Fatal(err)
		}
	}
	text, isErr := r.handlers["history"](context.Background(), map[string]any{"check": true, "since": "2026-11-01"})
	if isErr || !strings.Contains(text, `"checked"`) {
		t.Fatalf("history with check on a bound server: %s", text)
	}
	if q := f.log(); len(q) != 0 {
		t.Errorf("check on a bound server read lines it does not show: %v\n%s", q, text)
	}
}

func TestHistory_ErasedClusterIsAbsentFromCheckedInventory(t *testing.T) {
	for _, svc := range []string{"pg", "redis"} {
		t.Run(svc, func(t *testing.T) {
			checkAt(t)
			r := ownerRegistry(t, &ownerFake{clusterStatus: "ERASED"}, false, ownProject)
			eng, _ := r.newEngine(nil)
			dir := history.DirOf(intent.Dir(eng.StateDir))
			for i, tool := range []string{svc + "_create", svc + "_delete"} {
				line := history.Line{Time: "2026-11-17T10:00:00Z", Tool: tool, ProjectID: ownProject, Kind: resourceKind(tool), ResourceID: resMine, Outcome: history.Created, IntentID: fmt.Sprintf("a%015d", i), Key: eng.Maker()}
				if i == 1 {
					line.ProjectID = ""
					line.Outcome = history.Unknown
					line.Target = "cluster_id=" + resMine
				}
				if err := history.Append(dir, line); err != nil {
					t.Fatal(err)
				}
			}
			for _, check := range []bool{true, false} {
				ans, failed := r.handleHistory(context.Background(), map[string]any{"check": check})
				var out struct {
					Live       []historyLine
					WouldClose []historyLine `json:"would_close"`
				}
				if failed || json.Unmarshal([]byte(ans), &out) != nil || (check && (len(out.Live) != 0 || len(out.WouldClose) != 0)) || (!check && len(out.Live) != 2) {
					t.Fatalf("check=%v: %s", check, ans)
				}
			}
		})
	}
}

func TestHistoryCardRequiresMatchingIdentity(t *testing.T) {
	line := history.Line{ResourceID: resMine, ProjectID: ownProject}
	for _, k := range []*ownerKind{kindPG, historyVMCard} {
		for _, id := range []string{"", resOther} {
			if historyCardMatches(k, line, map[string]any{"id": id, "project_id": ownProject}) {
				t.Fatalf("accepted id %q for %s", id, line.ResourceID)
			}
		}
	}
	for _, project := range []string{"", ownOther} {
		if historyCardMatches(kindPG, line, map[string]any{"id": resMine, "project_id": project}) {
			t.Fatalf("accepted project %q for %s", project, line.ProjectID)
		}
	}
}
