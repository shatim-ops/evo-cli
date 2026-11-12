// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"strings"
	"testing"
)

// previewProject is the project a preview names where the CLI and the agent
// read it (namedProject): in the preview object where there is one, else
// beside the message; and the note that stands with or instead of it.
func previewProject(m map[string]any) (project, note string) {
	at := m
	if p, ok := m["preview"].(map[string]any); ok {
		at = p
	}
	project, _ = at["project_id"].(string)
	note, _ = at["project_note"].(string)
	return project, note
}

// A tool that takes consent and addresses a resource by id sends no
// project of its own: the write lands where the resource is. Such a tool
// declares which argument names that resource (idArg.target), so that its
// preview names the resource's project and not the call's (release plan
// 1.9, «Превью называет проект записи»). A tool whose request carries the
// project (projectTools, or the project_id argument) writes into the
// call's project and needs none. A new tool without the declaration fails
// here.
func TestPreviewProject_TargetDeclared(t *testing.T) {
	r := New(nil)
	for _, tl := range r.Tools() {
		targets := targetArgs(tl.Name)
		for _, a := range targets {
			if a.kind == nil || a.kind == kindOperation {
				t.Errorf("%s.%s: a target needs a kind that tells the project of the resource", tl.Name, a.name)
			} else if a.kind.project == nil {
				t.Errorf("%s.%s: the kind %q cannot tell the project of a resource", tl.Name, a.name, a.kind.what)
			}
		}
		if !takesConsent(tl) {
			if len(targets) > 0 {
				t.Errorf("%s takes no consent, so it has no preview: remove the target", tl.Name)
			}
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		_, namesItsProject := props["project_id"]
		if namesItsProject || projectTools[tl.Name] || len(targets) > 0 {
			continue
		}
		for _, a := range checkedIDArgs(tl.Name) {
			t.Errorf("%s addresses %s by id (%s) and sends no project: declare the argument the write lands in (idArg.target)",
				tl.Name, a.kind.what, a.name)
		}
	}
}

// previewCalls are the calls that reach a preview with their target alone:
// tool → the arguments beside the target.
var previewCalls = map[string]map[string]any{
	"pg_db_create":      {"name": "app", "owner": "app"},
	"pg_user_create":    {"name": "app", "password": "Preview-Pass-0"},
	"pg_backup_create":  {"name": "nightly"},
	"dns_record_create": {"name": "www", "record_type": "A", "value": "192.0.2.10"},
	"vpc_route_create":  {"subnet": "192.0.2.0/24", "next_hop_vip_id": resMine},
	"disk_resize":       {"size_gb": 100},
	// The tools that run a command in the cluster: a call that needs
	// consent. Without it nothing runs: the preview only.
	"k8s_kubectl": {"args": "delete pod web"},
	"k8s_apply":   {"manifest": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: web\n"},
	"k8s_helm":    {"action": "uninstall", "release": "web"},
}

// With a key of the organization and no --allow-project, the preview of a
// change addressed by id names the project the write lands in: the
// project of the resource where its API tells it, and the call's project
// only when the resource is the call's. Where the API tells no project and
// the call's project does not hold the resource, the preview says that the
// project is not determined — it never names the project of the profile
// for a resource of another project (the external review of 1.8.2: the
// operator confirmed the wrong target). The reason and the status of the
// answer stay as they were: only the preview's text changes.
func TestPreviewProject_NamesWhereTheWriteLands(t *testing.T) {
	stackHome(t)
	seen := 0
	reached := map[string]bool{}
	for _, c := range ownerCases(t) {
		if !c.arg.target {
			continue
		}
		for _, id := range []string{resMine, resForeign} {
			f := &ownerFake{}
			r := ownerRegistry(t, f, false, ownProject, ownOther, ownForeign)
			args := c.args(id)
			for k, v := range previewCalls[c.tool] {
				args[k] = v
			}
			m, text, _ := callOwned(t, r, c.tool, args, false)
			if m["status"] != "confirmation_required" {
				// Not a preview: a refusal or an answer with nothing to
				// consent to, which names no target to confirm.
				continue
			}
			seen++
			reached[c.tool] = true
			project, note := previewProject(m)
			switch {
			case id == resMine:
				if project != ownProject || note != "" {
					t.Errorf("%s %s=mine: project %q, note %q; want the call's project %s and no note\n%s",
						c.tool, c.arg.name, project, note, ownProject, text)
				}
			case isFieldKind(c.arg.kind):
				if project != ownForeign {
					t.Errorf("%s %s=foreign: project %q, want the resource's %s\n%s", c.tool, c.arg.name, project, ownForeign, text)
				}
				if note == "" {
					t.Errorf("%s %s=foreign: no note that it is not the call's project\n%s", c.tool, c.arg.name, text)
				}
			default:
				if project != "" || !strings.Contains(note, id) {
					t.Errorf("%s %s=foreign: project %q, note %q; want no project and a note naming %s\n%s",
						c.tool, c.arg.name, project, note, id, text)
				}
			}
			if id == resForeign && strings.Contains(text, ownProject) {
				t.Errorf("%s %s=foreign: the preview names the call's project %s\n%s", c.tool, c.arg.name, ownProject, text)
			}
			for _, q := range f.log() {
				if !strings.HasPrefix(q, "GET ") {
					t.Errorf("%s %s: the preview sent %s", c.tool, c.arg.name, q)
				}
			}
		}
	}
	if seen < 40 {
		t.Fatalf("only %d previews checked", seen)
	}
	// The calls named here must reach their preview: a tool whose call
	// stops being a preview would leave the check above without it.
	for tool := range previewCalls {
		if tool != "dns_record_create" && !reached[tool] {
			t.Errorf("%s: its call reached no preview, so its project was not checked", tool)
		}
	}
}

// The creates inside a parent of #616 reach their preview in this world,
// so the general test above checks them. dns_record_create is not here:
// it looks its zone up in the call's project and refuses a zone of
// another (NOT_FOUND), before any preview.
func TestPreviewProject_NestedCreatesReachPreview(t *testing.T) {
	stackHome(t)
	for _, tool := range []string{"pg_db_create", "pg_user_create", "pg_backup_create", "vpc_route_create"} {
		targets := targetArgs(tool)
		if len(targets) == 0 {
			t.Errorf("%s declares no target", tool)
			continue
		}
		f := &ownerFake{}
		r := ownerRegistry(t, f, false, ownProject, ownForeign)
		args := map[string]any{targets[0].name: resForeign}
		for k, v := range previewCalls[tool] {
			args[k] = v
		}
		m, text, _ := callOwned(t, r, tool, args, false)
		if m["status"] != "confirmation_required" {
			t.Errorf("%s: not a preview: %s", tool, text)
		}
	}
}

// The reads of the preview's project are made for a preview only: a call
// with consent sends what it sent before — the planner's read, the write
// and the poll of its operation, and no read of the project.
func TestPreviewProject_ConfirmedCallReadsNothingMore(t *testing.T) {
	for _, c := range []struct {
		tool, arg string
		want      []string
	}{
		{"pg_delete", "id", []string{
			"GET postgresql.api.cloud.ru/v1/clusters/" + resForeign,
			"DELETE postgresql.api.cloud.ru/v1/clusters/" + resForeign,
			"GET postgresql.api.cloud.ru/v1/operations/op-new",
		}},
		{"cert_renew", "certificate_id", []string{
			"POST certificatemanager.api.cloud.ru/v1/certificate/" + resForeign + ":renew",
		}},
	} {
		f := &ownerFake{}
		r := ownerRegistry(t, f, false, ownProject, ownForeign)
		_, text, isErr := callOwned(t, r, c.tool, map[string]any{c.arg: resForeign}, true)
		if isErr {
			t.Fatalf("%s: %s", c.tool, text)
		}
		if got := f.log(); !slices.Equal(got, c.want) {
			t.Errorf("%s: requests %v, want %v", c.tool, got, c.want)
		}
	}
}
