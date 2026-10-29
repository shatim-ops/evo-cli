// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
)

// namedProject is the handler of a create (createdIDs) whose preview names
// the project the call works in: a create in the wrong project is the
// costliest mistake of a create, and the project comes from the profile,
// the directory or the key, not from the call (release plan 1.6, «Превью
// create называет проект», coverage-1.5.md П6). A preview that names it
// already (engine.CreatePreview: project_id with project_from) is left as
// it is; any other gets project_id — one point for every create, not a
// line in each handler. Where the project comes from (project_from) waits
// for the human lines of the CLI (release plan 1.6, «Линия CLI»): the human
// preview does not show it yet (humanUnnamed, С2). The project is the call's: the
// project_id argument, else the configuration's; a create inside a parent
// addressed by id (a cluster, a zone, a balancer) writes where the parent
// is, which is the call's project unless an organization key reaches a
// parent of another project.
func (r *Registry) namedProject(next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		text, isErr := next(ctx, args)
		var m map[string]any
		if !isErr || json.Unmarshal([]byte(text), &m) != nil || m["status"] != "confirmation_required" || namesProject(m) {
			return text, isErr
		}
		project := stringArg(args, "project_id")
		if project == "" {
			eng, err := r.newEngine(nil)
			if err != nil || eng == nil {
				return text, isErr
			}
			project = eng.Config.ProjectID
		}
		if project == "" {
			return text, isErr
		}
		// Into the preview object where there is one (engine.CreatePreview:
		// the CLI shows its project line), else beside the message, where
		// the CLI shows every field.
		at := m
		if p, ok := m["preview"].(map[string]any); ok {
			at = p
		}
		at["project_id"] = project
		return toJSON(m), isErr
	}
}

// namesProject: the answer names a project at some depth (project_id or
// projectId, as the API and engine.CreatePreview spell it).
func namesProject(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if s, _ := e.(string); s != "" && (k == "project_id" || k == "projectId") {
				return true
			}
			if namesProject(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if namesProject(e) {
				return true
			}
		}
	}
	return false
}
