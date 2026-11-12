// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// doctorOnFake runs doctor on the registry of the fake of the owners; its
// probes of IAM and Compute go to a server that answers 404.
func doctorOnFake(t *testing.T, r *Registry) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	oldIAM, oldCompute := auth.IAMEndpoint, ComputeBaseURL
	t.Cleanup(func() { auth.IAMEndpoint, ComputeBaseURL = oldIAM, oldCompute })
	auth.IAMEndpoint, ComputeBaseURL = srv.URL+"/token", srv.URL
	text, _ := r.handlers["doctor"](context.Background(), map[string]any{})
	return text
}

// R-82 («Риск», conclusion 114): a delete addressed by id lands in the
// project of the resource, which a key of the organization reaches in any
// project. Its intent names no project — not the profile's — and the key
// it was sent with; cut after it went out, it is checked by a read of
// that id, not by the list of the call's project, where a resource of
// another project is «not listed» whatever happened to it. Only «not
// there», answered to the same key, closes the record.
func TestDoctor_CutDeleteByIDIsCheckedByID(t *testing.T) {
	this := config.MaskKeyID("k") // the key of the fake's engine
	for _, c := range []struct {
		name, tool, arg, id string
		status              int
		key                 *state.Maker // nil: the key of the call
		closed              bool
		says                string
	}{
		{name: "a resource of another project is still there", tool: "pg_delete", arg: "id", id: resForeign, says: "is still there"},
		{name: "not there, the same key", tool: "pg_delete", arg: "id", id: resGone, closed: true, says: "the delete took effect"},
		{name: "the other spelling of the argument", tool: "pg_delete", arg: "cluster_id", id: resGone, closed: true, says: "the delete took effect"},
		{name: "the read is refused", tool: "lb_delete", arg: "id", id: resGone, status: http.StatusForbidden, says: "could not read"},
		{name: "the read is throttled", tool: "vpc_delete", arg: "id", id: resGone, status: http.StatusTooManyRequests, says: "could not read"},
		{name: "the API fails", tool: "k8s_delete", arg: "cluster_id", id: resGone, status: http.StatusBadGateway, says: "could not read"},
		{name: "not there, another key", tool: "cert_delete", arg: "id", id: resGone,
			key: &state.Maker{ServiceAccountID: "sa-other", KeyID: "ot…er"}, says: "sent with another key"},
		{name: "a kind with no read by id", tool: "disk_delete", arg: "disk_id", id: resGone, says: "no read by id"},
		{name: "a kind with no read by id, of another project", tool: "kafka_delete", arg: "cluster_id", id: resForeign, says: "no read by id"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &ownerFake{goneStatus: c.status}
			r := ownerRegistry(t, f, false, ownProject)
			eng, _ := r.newEngine(nil)
			dir := intent.Dir(eng.StateDir)
			// The call was cut after it went out: its end never ran.
			if _, err := r.beginIntent(context.Background(), c.tool, map[string]any{c.arg: c.id, "confirmed": true}); err != nil {
				t.Fatal(err)
			}
			left := intent.Pending(dir)
			if len(left) != 1 {
				t.Fatalf("records: %+v", left)
			}
			in := left[0]
			if in.ProjectID != "" || in.Target != c.arg+"="+c.id {
				t.Errorf("the record names project %q, target %q; want no project and %s=%s", in.ProjectID, in.Target, c.arg, c.id)
			}
			if in.Key == nil || in.Key.KeyID != this {
				t.Errorf("the record's key: %+v, want the masked key of the call %s", in.Key, this)
			}
			if c.key != nil {
				// The same record, as another key left it.
				intent.Close(in)
				in.Key = c.key
				if _, err := intent.Begin(dir, in); err != nil {
					t.Fatal(err)
				}
			}
			text := doctorOnFake(t, r)
			if got := len(intent.Pending(dir)) == 0; got != c.closed {
				t.Errorf("closed %v, want %v\n%s", got, c.closed, text)
			}
			if !strings.Contains(text, c.says) {
				t.Errorf("doctor does not say %q:\n%s", c.says, text)
			}
			// A delete by id is not sent to the list of the call's project,
			// and a record of another key not to «the same delete», which
			// would close it unchecked («Риск», conclusion 128).
			if strings.Contains(text, "no read by id") && strings.Contains(text, "doctor checks it by id") {
				t.Errorf("doctor says both that it checks by id and that it has no read by id:\n%s", text)
			}
			if strings.Contains(text, "_list") {
				t.Errorf("doctor sends a delete by id to a list of the call's project:\n%s", text)
			}
			if c.key != nil && strings.Contains(text, "the same") {
				t.Errorf("doctor sends a record of another key to the same delete:\n%s", text)
			}
			if !c.closed && strings.Contains(text, "took effect") {
				t.Errorf("doctor calls a delete it did not check done:\n%s", text)
			}
			for _, q := range f.log() {
				if !strings.HasPrefix(q, "GET ") {
					t.Errorf("doctor sent %s", q)
				}
			}
		})
	}
}

// A call whose request carries its project keeps it in the record, and a
// delete inside a parent is not checked by a read of the parent: the
// parent being there says nothing of the child.
func TestIntent_ProjectOfTheRequestStays(t *testing.T) {
	f := &ownerFake{}
	r := ownerRegistry(t, f, false, ownProject)
	eng, _ := r.newEngine(nil)
	dir := intent.Dir(eng.StateDir)
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"pg_create", map[string]any{"name": "db"}, ownProject},
		{"ar_delete", map[string]any{"id": resMine}, ownProject},
		{"pg_db_delete", map[string]any{"cluster_id": resForeign, "name": ownerChild}, ""},
	} {
		if _, err := r.beginIntent(context.Background(), c.tool, c.args); err != nil {
			t.Fatal(err)
		}
		var got *intent.Intent
		for _, in := range intent.Pending(dir) {
			if in.Tool == c.tool {
				got = &in
			}
		}
		if got == nil || got.ProjectID != c.want {
			t.Errorf("%s: record %+v, want project %q", c.tool, got, c.want)
		}
	}
	text := doctorOnFake(t, r)
	if strings.Contains(text, "took effect") || strings.Contains(text, "is still there") {
		t.Errorf("doctor read the parent of a child's delete, or closed a record:\n%s", text)
	}
	if n := len(intent.Pending(dir)); n != 3 {
		t.Errorf("%d records left, want 3\n%s", n, text)
	}
}

// The rest of R-82 («Риск», conclusion 128): a record another key left is
// not closed by the same delete with this key — which may not see the
// resource, and whose already_deleted is a 404 of its own view. The record
// stays and the answer names it; a record of this key, or one that names
// no key (evo before 1.9), closes as before.
func TestIntent_RepeatDoesNotCloseARecordOfAnotherKey(t *testing.T) {
	for _, c := range []struct {
		name   string
		key    *state.Maker
		closed bool
	}{
		{"another key", &state.Maker{ServiceAccountID: "sa-other", KeyID: "ot…er"}, false},
		{"this key", &state.Maker{KeyID: config.MaskKeyID("k")}, true},
		{"no key in the record", nil, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &ownerFake{}
			r := ownerRegistry(t, f, false, ownProject)
			eng, _ := r.newEngine(nil)
			dir := intent.Dir(eng.StateDir)
			if _, err := intent.Begin(dir, intent.Intent{Tool: "pg_delete", Target: "id=" + resGone, Key: c.key}); err != nil {
				t.Fatal(err)
			}
			m, text, isErr := callOwned(t, r, "pg_delete", map[string]any{"id": resGone}, true)
			if isErr || m["status"] != "already_deleted" {
				t.Fatalf("the repeat: %s", text)
			}
			left := intent.Pending(dir)
			if got := len(left) == 0; got != c.closed {
				t.Errorf("closed %v, want %v: %+v", got, c.closed, left)
			}
			named := strings.Contains(text, "pending_operations") && strings.Contains(text, "sent with another key")
			if named == c.closed {
				t.Errorf("the answer names a kept record: %v, want %v\n%s", named, !c.closed, text)
			}
			if !c.closed {
				// Its words send to doctor with that key, not to the same call.
				note := r.PendingNotes(eng.StateDir)
				if len(note) != 1 || strings.Contains(note[0], "the same") || !strings.Contains(note[0], "ot…er") {
					t.Errorf("the note of the kept record: %v", note)
				}
			}
		})
	}
}
