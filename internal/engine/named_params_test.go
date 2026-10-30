// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

// Release plan 1.6, «Линия CLI»: a reference and its name are one line
// «name (id)» for a person, and stay two keys of the JSON preview (the
// answer of MCP and of --json) with their values as before.
func TestNamedParams_HumanOnly(t *testing.T) {
	p := CreatePreview{Resource: ResourceSubnet, Name: "web"}
	p.Add("vpc_id", "v1")
	p.Add("vpc_name", "main")
	before, _ := json.Marshal(p)
	lines := strings.Join(ShowCreatePreview("create", p).Lines, "\n")
	if !strings.Contains(lines, "main (v1)") || strings.Contains(lines, "vpc-name") {
		t.Errorf("human preview:\n%s", lines)
	}
	after, _ := json.Marshal(p)
	if string(after) != string(before) {
		t.Errorf("the JSON preview changed: %s, was %s", after, before)
	}
	var j struct{ Params []PreviewParam }
	_ = json.Unmarshal(after, &j)
	got := map[string]any{}
	for _, prm := range j.Params {
		got[prm.Key] = prm.Value
	}
	if got["vpc_id"] != "v1" || got["vpc_name"] != "main" {
		t.Errorf("JSON params %v: want vpc_id v1 and vpc_name main, apart", got)
	}
}

// A reference whose name is the key without "_id" (flavor beside
// flavor_id, as k8s answers it) reads «name (id)» too; the name's own
// line goes.
func TestNamedParams_NameWithoutSuffix(t *testing.T) {
	got := namedParams([]PreviewParam{{Key: "flavor", Value: "gen-2-4"}, {Key: "flavor_id", Value: "f1"}, {Key: "zone", Value: "ru.AZ-1"}})
	if len(got) != 2 || got[0].Key != "flavor_id" || got[0].Value != "gen-2-4 (f1)" || got[1].Key != "zone" {
		t.Errorf("params %+v", got)
	}
}
