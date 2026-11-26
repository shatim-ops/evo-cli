// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
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
	const unresolved = "00000000-0000-4000-8000-0000000000cc"
	factory := r.newEngine
	r.newEngine = func(over map[string]any) (*engine.Engine, error) {
		if over["ProjectID"] == unresolved {
			return nil, errors.New("profile unavailable")
		}
		return factory(over)
	}
	for i, project := range []string{"00000000-0000-4000-8000-0000000000bb", unresolved, ""} {
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
					if svc == "pg" { // old pg_delete stored the ID only in target
						line.ResourceID = ""
					}
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
			if historyCardMatches(k, line, map[string]any{"id": id, "project_id": ownProject}, ownProject) {
				t.Fatalf("accepted id %q for %s", id, line.ResourceID)
			}
		}
	}
	for _, project := range []string{"", ownOther} {
		if historyCardMatches(kindPG, line, map[string]any{"id": resMine, "project_id": project}, ownProject) {
			t.Fatalf("accepted project %q for %s", project, line.ProjectID)
		}
	}
	line.ProjectID = ""
	for _, k := range []*ownerKind{kindPG, kindRedis} {
		card := map[string]any{"id": resMine, "project_id": ownProject}
		for _, active := range []string{"", ownOther} {
			if historyCardMatches(k, line, card, active) {
				t.Errorf("projectless line matched card of %q when active project was %q", ownProject, active)
			}
		}
	}
}

func TestHistoryRedisProjectMembership(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lastPage string
		status   int
		pages    int
		gone     bool
	}{
		{"found", "{}", 200, 2, true},
		{"missing", "{}", 200, 2, false},
		{"late-null", "null", 200, 2, false},
		{"unidentified member", `{"clusters":[{"name":"unidentified"}]}`, 200, 2, false},
		{"forbidden", "{}", 403, 2, false},
		{"list-404", "{}", 404, 2, false},
		{"budget", "{}", 200, historyCheckMax - 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkAt(t)
			r := ownerRegistry(t, &ownerFake{}, false, ownProject)
			eng, err := r.newEngine(nil)
			if err != nil {
				t.Fatal(err)
			}
			l := history.Line{Format: history.Format, Time: "2026-11-17T10:00:00Z", Tool: "redis_create", Kind: resourceKind("redis_create"), ResourceID: resMine, ProjectID: ownProject, Key: eng.Maker(), Outcome: history.Created}
			pages := 0
			httpretry.DefaultBase = &apiFake{own: func(w http.ResponseWriter, req *http.Request) bool {
				if req.Method != "GET" {
					t.Fatalf("unexpected %s", req.Method)
				}
				switch req.URL.Path {
				case "/v1/clusters/" + resMine:
					fmt.Fprintf(w, `{"id":%q,"status":"ERASED"}`, resMine)
				case "/v1/clusters":
					if req.URL.Query().Get("project_id") != ownProject {
						t.Fatal(req.URL)
					}
					pages++
					if pages == 1 || tc.name == "budget" {
						id := resMine
						if tc.name == "missing" {
							id = resOther
						} else if pages > 1 {
							id = fmt.Sprintf("00000000-0000-4000-8000-%012d", pages)
						}
						fmt.Fprintf(w, `{"clusters":[{"id":%q}],"next_page_token":"%d"}`, id, pages)
					} else {
						w.WriteHeader(tc.status)
						fmt.Fprint(w, tc.lastPage)
					}
				default:
					t.Fatalf("unexpected path %s", req.URL)
				}
				return true
			}}
			out, results := r.checkLive(context.Background(), []history.Line{l}, []int{0}, eng)
			want := checkReadFailed
			if tc.gone {
				want = checkGone
			}
			if pages != tc.pages || out.Read != 1 || results[0] != want || out.WouldClose != 0 {
				t.Fatalf("pages=%d checked=%+v results=%v", pages, out, results)
			}
		})
	}
}

// An invalid membership page cannot establish the project for either card.
// Both checks share one failed inventory read, so uncertainty costs no retry.
func TestHistoryRedisCachesIncompleteMembership(t *testing.T) {
	checkAt(t)
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, err := r.newEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	line := history.Line{Format: history.Format, Time: "2026-11-17T10:00:00Z", Tool: "redis_create", Kind: resourceKind("redis_create"), ProjectID: ownProject, Key: eng.Maker(), Outcome: history.Created}
	lines := []history.Line{line, line}
	lines[0].ResourceID = resMine
	lines[1].ResourceID = resOther
	pages, cards := 0, 0
	httpr := &apiFake{own: func(w http.ResponseWriter, req *http.Request) bool {
		if req.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", req.Method)
		}
		switch req.URL.Path {
		case "/v1/clusters/" + resMine, "/v1/clusters/" + resOther:
			cards++
			fmt.Fprintf(w, `{"id":%q,"status":"ERASED"}`, strings.TrimPrefix(req.URL.Path, "/v1/clusters/"))
		case "/v1/clusters":
			pages++
			fmt.Fprintf(w, `{"clusters":[{"id":%q},{"id":%q},{"name":"unidentified"}]}`, resMine, resOther)
		default:
			t.Fatalf("unexpected %s", req.URL.Path)
		}
		return true
	}}
	httpretry.DefaultBase = httpr
	out, result := r.checkLive(context.Background(), lines, []int{0, 1}, eng)
	if cards != 2 || pages != 1 || result[0] != checkReadFailed || result[1] != checkReadFailed || out.WouldClose != 0 {
		t.Fatalf("cards=%d membership=%d checks=%v results=%v", cards, pages, out, result)
	}
}

// The terminal card must belong to the same project even when an old
// deletion entry has no project of its own. An earlier entry for that
// resource inherits only a terminal result whose scope was verified.
func TestHistory_LegacyClusterDeletionScope(t *testing.T) {
	for _, tool := range []string{"pg_delete", "redis_delete"} {
		for _, tc := range []struct {
			name, recorded, card                   string
			bound                                  bool
			entries, wantLive, wantGone, wantReads int
		}{
			{"legacy-own", "", ownProject, false, 1, 0, 1, 1},
			{"legacy-other", "", ownOther, false, 1, 1, 0, 1},
			{"legacy-unknown", "", "", false, 1, 1, 0, 1},
			{"legacy-other-supersedes", "", ownOther, false, 2, 2, 0, 1},
			{"recorded-own", ownProject, ownProject, false, 1, 0, 1, 1},
			{"recorded-other", ownProject, ownOther, false, 1, 1, 0, 1},
			{"bounded-legacy", "", ownOther, true, 1, 0, 0, 0},
		} {
			t.Run(tool+"/"+tc.name, func(t *testing.T) {
				checkAt(t)
				r := ownerRegistry(t, &ownerFake{}, tc.bound, ownProject)
				eng, err := r.newEngine(nil)
				if err != nil {
					t.Fatal(err)
				}
				hdir := history.DirOf(intent.Dir(eng.StateDir))
				for i := 0; i < tc.entries; i++ {
					line := history.Line{
						Time: fmt.Sprintf("2026-11-17T10:0%d:00Z", i), Tool: tool, Kind: resourceKind(tool),
						ResourceID: resMine, ProjectID: tc.recorded, Outcome: history.Unknown,
						Key: eng.Maker(), IntentID: fmt.Sprintf("%016x", i+1),
					}
					// Older PostgreSQL entries stored the ID in the target only.
					if tool == "pg_delete" && tc.recorded == "" && i == 0 {
						line.Target = "cluster_id=" + resMine
						line.ResourceID = ""
					}
					if err := history.Append(hdir, line); err != nil {
						t.Fatal(err)
					}
				}
				reads := 0
				httpretry.DefaultBase = &apiFake{own: func(w http.ResponseWriter, req *http.Request) bool {
					if req.Method != http.MethodGet {
						t.Fatalf("unexpected %s", req.Method)
					}
					switch req.URL.Path {
					case "/v1/clusters/" + resMine:
						reads++
						fmt.Fprintf(w, `{"id":%q,"project_id":%q,"status":"DELETED"}`, resMine, tc.card)
					case "/v1/clusters":
						// The fallback project list for a legacy Redis card
						// contains no matching resource.
						if tool != "redis_delete" || req.URL.Query().Get("project_id") != ownProject {
							t.Fatalf("unexpected scoped list %v", req.URL)
						}
						fmt.Fprint(w, `{"clusters":[]}`)
					default:
						t.Fatalf("unexpected read %s", req.URL)
					}
					return true
				}}
				text, failed := r.handlers["history"](context.Background(), map[string]any{"check": true, "since": "2026-11-01"})
				if failed {
					t.Fatal(text)
				}
				var got struct {
					Live    []historyLine
					Checked historyChecked
				}
				if err := json.Unmarshal([]byte(text), &got); err != nil {
					t.Fatal(err)
				}
				if reads != tc.wantReads || got.Checked.Gone != tc.wantGone || got.Checked.WouldClose != 0 || len(got.Live) != tc.wantLive {
					t.Fatalf("GET by ID=%d, gone=%d, would_close=%d, live=%d; want %d, %d, 0, %d",
						reads, got.Checked.Gone, got.Checked.WouldClose, len(got.Live), tc.wantReads, tc.wantGone, tc.wantLive)
				}
				if len(got.Live) > 0 && got.Live[len(got.Live)-1].CheckResult != checkReadFailed {
					t.Fatalf("latest live result=%q; want %q", got.Live[len(got.Live)-1].CheckResult, checkReadFailed)
				}
				if len(got.Live) == 2 && got.Live[0].CheckResult != checkLater {
					t.Fatalf("earlier live result=%q; want %q", got.Live[0].CheckResult, checkLater)
				}
			})
		}
	}
}
