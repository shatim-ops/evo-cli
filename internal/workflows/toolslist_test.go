// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import "testing"

// The ratchet of tools/list (owner's decision 29.10, #597, item 12): no
// number in the code; a workflow of the pull request, rerun when its
// description is edited, compares tools/list of the head with its base and
// wants the line that explains a growth. The head is the merge commit the
// event checks out, and its base is that commit's first parent: the base
// branch as the merge saw it. base.sha of the event may be older (run on
// #649: +157 bytes main had gained, not the PR), and a merge-base with it
// counts main's growth against the PR.
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
	for _, want := range []string{"git rev-parse HEAD^1", ".github/scripts/tools_list_growth.sh"} {
		if !runsStep(w, want) {
			t.Errorf("tools-list: no step runs %s", want)
		}
	}
	if runsStep(w, "git merge-base") || runsStep(w, "pull_request.base.sha") {
		t.Error("tools-list: the base is the first parent of the merge commit, not base.sha of the event")
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
