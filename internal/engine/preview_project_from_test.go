// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Every origin a preview's project_from can carry is said in words on the
// project line of the human preview: the person sees where the project of
// the write comes from, as the agent does in the details (the human
// preview test takes project_from for a form key on this test's word).
// The project has no default value, so OriginDefault never comes with an
// id: the line says «(not set)».
func TestProjectText_SaysEveryOriginInWords(t *testing.T) {
	for _, from := range []string{config.OriginProfile, config.OriginProject, config.OriginEnv, config.OriginFlag,
		originRecord, originPlan, OriginResource} {
		line := projectText("p-1", from)
		if !strings.HasPrefix(line, "p-1 (") || len(line) <= len("p-1 ()") {
			t.Errorf("project_from %q: the line %q does not say it", from, line)
		}
	}
	if got := projectText("", config.OriginDefault); !strings.Contains(got, "not set") && !strings.Contains(got, "не задан") {
		t.Errorf("no project: %q", got)
	}
}

func TestPendingRulesPreview_OnlyOneRecordedProjectAndVMIdentifiesTheIntent(t *testing.T) {
	sf := &state.File{Deployments: []state.DeploymentRecord{
		{ID: "one", ProjectID: "project", VMID: "vm"},
		{ID: "other", ProjectID: "project", VMID: "another-vm"},
		{ID: "no-project", VMID: "vm"},
		{ID: "no-vm", ProjectID: "project"},
	}}
	if got := pendingRulesPreview(sf, []string{"one"}).TargetArgs; got["vm_id"] != "vm" || got["project_id"] != "project" {
		t.Fatalf("single known receipt: %+v", got)
	}
	for _, ids := range [][]string{{"one", "other"}, {"no-project"}, {"no-vm"}, {"unknown"}} {
		if got := pendingRulesPreview(sf, ids).TargetArgs; len(got) != 0 {
			t.Errorf("ambiguous or missing receipt %v claimed target %+v", ids, got)
		}
	}
}
