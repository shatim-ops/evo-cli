// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// previewNoProject are the creates whose preview does not name the project
// yet, → the work item that names it. The list only shrinks: a create not
// here whose preview leaves the project out fails the test, and so does a
// line whose preview names it.
var previewNoProject = map[string]string{}

// Release plan 1.6, «Превью create называет проект» (coverage-1.5.md, П6):
// a create in the wrong project is the costliest mistake of a create, and
// the project comes from the profile or the key, not from the call. Every
// create (docs/internal/contract-map.json, "creates") names in its preview
// the project it writes to: a key project, project_id or projectId of the
// preview (any depth, or a parameter of the preview) with the project of
// the configuration. On the world of TestParity_JSONEqualsTool. The creates
// whose preview is engine.CreatePreview (provision, deploy, disk_create,
// pg_create, k8s_*) name it already, as project_id with project_from; the
// others build their own.
func TestPreview_CreateNamesTheProject(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "internal", "contract-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	var declared map[string]struct {
		Creates *struct{} `json:"creates"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		t.Fatal(err)
	}
	creates := map[string]bool{}
	for name, d := range declared {
		if d.Creates != nil {
			creates[name] = true
		}
	}
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	reg := newRegistry()
	missing, seen := map[string]bool{}, map[string]bool{}
	for _, c := range cases {
		if !creates[c.tool.Name] || seen[c.tool.Name] || c.state != "" {
			continue
		}
		seen[c.tool.Name] = true
		tl, _ := reg.Lookup(c.tool.Name)
		args := copyArgs(c.args)
		done := func() {}
		if c.setup != nil {
			extra, d := c.setup(t, w)
			for k, v := range extra {
				args[k] = v
			}
			done = d
		}
		fake.reset()
		newProcess()
		flagJSON, flagDryRun, flagYes = false, false, false
		preview, _ := tl.Handler(context.Background(), args)
		fake.takeWrites()
		done()
		_ = os.Remove(state.DefaultPath(w.dir))
		if !namedValues(preview, nil)["projectid"][pvProject] && !namedValues(preview, nil)["project"][pvProject] {
			missing[c.tool.Name] = true
		}
	}
	var bad []string
	for name := range creates {
		switch {
		case !seen[name]:
			bad = append(bad, name+": a create without a case in parityArgs")
		case missing[name] && previewNoProject[name] == "":
			bad = append(bad, name+": the preview does not name the project "+pvProject+" — name it, or a line of previewNoProject with the work item")
		case !missing[name] && previewNoProject[name] != "":
			bad = append(bad, name+": in previewNoProject, but its preview names the project: remove the line")
		}
	}
	for name := range previewNoProject {
		if !creates[name] {
			bad = append(bad, name+": in previewNoProject, but no create: remove the line")
		}
	}
	t.Logf("creates: %d; previews without the project: %d", len(seen), len(missing))
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}
