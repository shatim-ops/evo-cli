// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

func TestJournalProvenanceDeletePreview(t *testing.T) {
	for _, c := range []struct{ name, create, project, answer, want string }{
		{"Redis create receipt", "redis_create", ownProject, `{"status":"creating","operation_id":"op","cluster_id":"ID"}`, "yes"},
		{"subnet create receipt", "subnet_create", ownProject, `{"status":"created","subnet_id":"ID","subnet":{"id":"ID"}}`, "yes"},
		{"subnet found after lost response", "subnet_create", ownProject, `{"status":"creating","subnet_id":"ID","warning":"lost"}`, "unknown"},
		{"Redis found after lost response", "redis_create", ownProject, `{"status":"creating","cluster_id":"ID","warning":"lost"}`, "unknown"},
		{"another project", "redis_create", ownForeign, `{"status":"creating","operation_id":"op","cluster_id":"ID"}`, "unknown"},
		{"same name other id", "redis_create", ownProject, `{"status":"creating","operation_id":"op","cluster_id":"other-id"}`, "unknown"},
		{"found", "redis_create", ownProject, `{"status":"already_exists","cluster_id":"ID","operation_id":"op"}`, "unknown"},
		{"unknown", "redis_create", ownProject, `{"status":"error","reason":"API_ERROR","cluster_id":"ID","operation_id":"op"}`, "unknown"},
		{"old subnet line without receipt", "subnet_create", ownProject, `{"status":"created","subnet_id":"ID"}`, "unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &ownerFake{forbidDelete: true}
			r := ownerRegistry(t, f, false, ownProject)
			root, _ := r.stateDir()
			dir := intent.Dir(root)
			in, err := intent.Open(dir, intent.Intent{Tool: c.create, ProjectID: c.project, Surface: "cli", Name: "res-a1"})
			if err != nil {
				t.Fatal(err)
			}
			out := historyOutcome(c.create, nil, strings.ReplaceAll(c.answer, "ID", resMine))
			intent.Close(in, out)
			hdir := history.DirOf(dir)
			before, err := history.ReadAll(hdir, nil)
			if err != nil || len(before.Lines) != 1 {
				t.Fatalf("journal: %+v %v", before, err)
			}
			del := decls[c.create].Created.delete
			m, text, _ := callNamed(t, r, del, map[string]any{decls[del].Delete.arg: resMine})
			prov, _ := m["provenance"].(map[string]any)
			if m["status"] != "confirmation_required" || prov["created_by_evo"] != c.want {
				t.Fatalf("want %s: %s", c.want, text)
			}
			after, err := history.ReadAll(hdir, nil)
			if err != nil || !reflect.DeepEqual(before.Lines, after.Lines) {
				t.Fatalf("preview changed journal: %+v %v", after, err)
			}
			for _, req := range f.log() {
				if !strings.HasPrefix(req, "GET ") {
					t.Fatalf("preview sent %s", req)
				}
			}
			if c.want == "yes" {
				_, response, failed := callNamed(t, r, del, map[string]any{decls[del].Delete.arg: resMine, "confirmed": true})
				if !failed || !strings.Contains(response, "permission denied") {
					t.Fatalf("expected IAM refusal: %s", response)
				}
			}
		})
	}
}

func TestJournalProvenanceAfterCLICreate(t *testing.T) {
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"redis_create", map[string]any{"name": "cache", "cpu": 2, "ram": 4, "confirmed": true}},
		{"subnet_create", map[string]any{"name": "web", "cidr": "10.10.0.0/24", "zone": "ru.AZ-1", "vpc_id": faultVPC, "confirmed": true}},
	} {
		t.Run(c.tool, func(t *testing.T) {
			world := &faultAPI{made: []faultResource{{host: "vpc.api.cloud.ru", path: "/v1/vpcs", ID: faultVPC, Name: "net", before: true}}}
			r := faultAPIRegistry(t, world)
			create, _ := r.Lookup(c.tool)
			answer, failed := create.Handler(WithCaller(context.Background(), &Caller{Yes: true}), c.args)
			var result map[string]any
			if failed || json.Unmarshal([]byte(answer), &result) != nil {
				t.Fatal(answer)
			}
			id := createdID(c.tool, result)
			del := decls[c.tool].Created.delete
			args := map[string]any{decls[del].Delete.arg: id}
			before := len(world.callLog())
			m, text, _ := callNamed(t, r, del, args)
			prov, _ := m["provenance"].(map[string]any)
			if id == "" || m["status"] != "confirmation_required" || prov["created_by_evo"] != "yes" {
				t.Fatalf("create: %s; preview: %s", answer, text)
			}
			for _, request := range world.callLog()[before:] {
				if !strings.HasPrefix(request, "GET ") {
					t.Fatalf("preview sent %s", request)
				}
			}
		})
	}
}

func TestJournalProvenanceRejectsUnrelatedReceipts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*history.Line)
	}{
		{"another folder", func(l *history.Line) { l.StateDir += "-other" }},
		{"folder not recorded", func(l *history.Line) { l.StateDir = "" }},
		{"different kind", func(l *history.Line) { l.Kind = resourceKind("redis_create") }},
		{"different create", func(l *history.Line) { l.Tool = "redis_create" }},
		{"found", func(l *history.Line) { l.Outcome = history.Found }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ownerRegistry(t, &ownerFake{}, false, ownProject)
			root, _ := r.stateDir()
			line := history.Line{Time: "2026-11-17T10:00:00Z", Tool: "subnet_create", Kind: resourceKind("subnet_create"), IntentID: "a000000000000001", StateDir: root, ProjectID: ownProject, ResourceID: resMine, Outcome: history.Created, CreateReceipt: true}
			tc.alter(&line)
			if err := history.Append(history.DirOf(intent.Dir(root)), line); err != nil {
				t.Fatal(err)
			}
			if p := r.journalProvenance("subnet_delete", ownProject, resMine); p.CreatedByEvo != "unknown" {
				t.Fatalf("unexpected provenance: %+v", p)
			}
		})
	}
}
