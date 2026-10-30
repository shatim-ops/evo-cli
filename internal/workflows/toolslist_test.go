// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import "testing"

// The ratchet of tools/list (owner's decision 29.10, #597, item 12): no
// number in the code; a workflow of the pull request, rerun when its
// description is edited, compares tools/list of the head with the
// merge-base and wants the line that explains a growth.
func TestWorkflows_ToolsListGrowthChecked(t *testing.T) {
	w, ok := readWorkflows(t)["tools-list.yml"]
	if !ok {
		t.Fatal("no tools-list.yml")
	}
	pr, _ := w.On["pull_request"].(map[string]any)
	types, _ := pr["types"].([]any)
	if !slicesContain(types, "edited") || !slicesContain(types, "synchronize") {
		t.Errorf("tools-list: pull_request types %v, want edited and synchronize among them", types)
	}
	for _, want := range []string{"git merge-base", ".github/scripts/tools_list_growth.sh"} {
		if !runsStep(w, want) {
			t.Errorf("tools-list: no step runs %s", want)
		}
	}
}

func slicesContain(xs []any, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
