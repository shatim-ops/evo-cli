// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// history with check reads each live line it can by id (readByID, the rule
// of doctor) and closes the line of a resource the read says is gone,
// with a line not_found of the same resource (the ТЗ of the journal, У7):
// a resource removed in the console is no longer live. Only «not there»,
// answered to the key that sent the call, closes; any other answer keeps
// the line live and says why. A line whose id is a name (an outcome not
// known of a create by name) is not read by id. The cloud is only read.
func TestHistory_CheckClosesWhatAReadByIDFindsGone(t *testing.T) {
	this := &state.Maker{KeyID: config.MaskKeyID("k")} // the key of the fake's engine
	other := &state.Maker{ServiceAccountID: "sa-other", KeyID: "ot…er"}
	for _, c := range []struct {
		name, tool, id, outcome string
		key                     *state.Maker
		status                  int
		closed                  bool
		says                    string
	}{
		{name: "gone, the same key", tool: "pg_create", id: resGone, outcome: history.Created, key: this, closed: true},
		{name: "found by a repeat, gone", tool: "lb_create", id: resGone, outcome: history.Found, key: this, closed: true},
		{name: "still there", tool: "pg_create", id: resForeign, outcome: history.Created, key: this, says: checkThere},
		{name: "the read is refused", tool: "lb_create", id: resGone, outcome: history.Created, key: this, status: http.StatusForbidden, says: checkReadFailed},
		{name: "the read is throttled", tool: "vpc_create", id: resGone, outcome: history.Created, key: this, status: http.StatusTooManyRequests, says: checkReadFailed},
		{name: "another key", tool: "cert_import", id: resGone, outcome: history.Created, key: other, says: checkOtherKey},
		{name: "a line with no key", tool: "pg_create", id: resGone, outcome: history.Created, says: checkNoKey},
		{name: "a kind with no read by id", tool: "disk_create", id: resGone, outcome: history.Created, key: this, says: checkNoRead},
		{name: "an outcome not known: the id may be a name", tool: "pg_create", id: resGone, outcome: history.Unknown, key: this, says: checkNotByID},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &ownerFake{goneStatus: c.status}
			r := ownerRegistry(t, f, false, ownProject)
			eng, _ := r.newEngine(nil)
			hdir := history.DirOf(intent.Dir(eng.StateDir))
			line := history.Line{Time: "2026-11-17T10:00:00Z", Tool: c.tool, ProjectID: ownProject, Kind: resourceKind(c.tool),
				ResourceID: c.id, Outcome: c.outcome, IntentID: "a000000000000001", Key: c.key}
			if err := history.Append(hdir, line); err != nil {
				t.Fatal(err)
			}
			text, isErr := r.handlers["history"](context.Background(), map[string]any{"check": true, "since": "2026-11-01"})
			if isErr {
				t.Fatal(text)
			}
			var a struct {
				Live    []historyLine `json:"live"`
				Closed  []historyLine `json:"closed"`
				Checked struct {
					Read       int `json:"read"`
					Closed     int `json:"closed"`
					NotChecked int `json:"not_checked"`
				} `json:"checked"`
			}
			if err := json.Unmarshal([]byte(text), &a); err != nil {
				t.Fatal(err)
			}
			j, _ := history.ReadAll(hdir, nil)
			notFound := 0
			for _, l := range j.Lines {
				if l.Outcome == history.NotFound && l.ResourceID == c.id && l.Kind == line.Kind && l.ProjectID == ownProject {
					notFound++
				}
			}
			if c.closed {
				if len(a.Live) != 0 || len(a.Closed) != 1 || notFound != 1 || len(history.Live(j.Lines)) != 0 {
					t.Errorf("live %+v, closed %+v, not_found lines %d; want the line closed, with one line not_found\n%s", a.Live, a.Closed, notFound, text)
				}
				// A second check reads nothing: the line is no longer live.
				before := len(f.log())
				_, _ = r.handlers["history"](context.Background(), map[string]any{"check": true, "since": "2026-11-01"})
				if len(f.log()) != before {
					t.Errorf("a second check read %v again", f.log()[before:])
				}
			} else {
				if len(a.Live) != 1 || a.Live[0].CheckResult != c.says || notFound != 0 {
					t.Errorf("live %+v, not_found lines %d; want the line live with check_result %q\n%s", a.Live, notFound, c.says, text)
				}
			}
			for _, q := range f.log() {
				if !strings.HasPrefix(q, "GET ") {
					t.Errorf("history with check sent %s", q)
				}
			}
		})
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
	j, _ := history.ReadAll(hdir, nil)
	if len(history.Live(j.Lines)) != 2 {
		t.Errorf("a line it does not show was closed: %+v", j.Lines)
	}
}
