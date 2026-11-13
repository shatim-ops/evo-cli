// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// An answer that says "_untrusted" names every field that carries text of
// the cloud (release plan 1.9, «Находки промежуточного дельта-аудита»,
// class 3; FB19I-21, -9, -14): TestUntrustedNames_Marked holds that an
// answer with a name of the cloud has the marker at all, and the list of
// the fields under it (untrustedFields) is kept by hand — three new answers
// had the marker and a name of the cloud in a field it did not name. A
// client that trusts the marker reads that field as evo's. The world is the
// same: every list and every read of every service names its resource
// hostileName, and so do the resources inside it — the VMs of a group, the
// plan of a backup, the target of a delete.
//
// What it reaches: the tools the world's one body and requiredArgs bring to
// an answer with the marker — a tool whose call needs more than its
// required arguments answers USAGE here and is not read (open_ports and
// k8s_nodepool_delete, FB19I-14 and FB19I-9, among them: their worlds are
// their own tests'). The count is logged.

// untrustedFieldPending are the fields known to carry a name of the cloud
// outside the "_untrusted" of their answer, "tool: field" → the finding.
// Only shrinks. Closes by 1.9, before rc.1 (release plan 1.9, «Сроки
// храповиков «к 1.7–1.9»»).
var untrustedFieldPending = map[string]string{
	"backup_get: protection_plan": "FB19I-21 of the interim delta audit of 1.9: the name of the plan is the cloud's; «разработка B»",
}

// hostileNested adds to hostileBody the resources an answer of the cloud
// names inside another, each named hostileName.
func hostileNested() map[string]any {
	named := map[string]any{"id": "00000000-0000-4000-8000-0000000000f1", "name": hostileName}
	return map[string]any{
		"protection_plan": named, "security_group": named, "resource": named, "vm": named,
		"interfaces": []any{named}, "nodePools": []any{named}, "disks": []any{named},
	}
}

// carriers are the fields of the answer m whose value carries the hostile
// name, but for evo's own text (textFields), where it is quoted.
func carriers(m map[string]any) []string {
	var out []string
	for field, v := range m {
		if field == "_untrusted" || textFields[field] || field == "warnings" {
			continue
		}
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), hostileMarker) {
			out = append(out, field)
		}
	}
	sort.Strings(out)
	return out
}

func TestUntrusted_EveryFieldWithTextOfTheCloudIsNamed(t *testing.T) {
	faultToolsIsolate(t)
	api := &oddAPI{status: http.StatusOK, body: hostileBody(hostileNested())}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)
	found := map[string]bool{}
	withMarker := 0
	for _, tl := range r.Tools() {
		m, text, _ := callNamed(t, r, tl.Name, requiredArgs(tl))
		list, ok := m["_untrusted"].([]any)
		if !ok {
			continue // no marker at all: TestUntrustedNames_Marked and namesUnmarked
		}
		named := map[string]bool{}
		for _, f := range list {
			if s, ok := f.(string); ok {
				named[s] = true
			}
		}
		withMarker++
		for _, field := range carriers(m) {
			if named[field] {
				continue
			}
			key := tl.Name + ": " + field
			found[key] = true
			if untrustedFieldPending[key] == "" {
				t.Errorf("%s: the field %q carries a name of the cloud, and \"_untrusted\" (%v) does not name it — add it to untrustedFields\n%.400s", tl.Name, field, list, text)
			}
		}
	}
	for key := range untrustedFieldPending {
		if !found[key] {
			t.Errorf("untrustedFieldPending names %s, which is named in \"_untrusted\" now or carries no name of the cloud: remove the line (the list only shrinks)", key)
		}
	}
	if withMarker < 10 {
		t.Errorf("%d answers say \"_untrusted\": the world no longer reaches the answers, and the check reads nothing", withMarker)
	}
	t.Logf("answers with the marker: %d; fields outside it: %d", withMarker, len(found))
}
