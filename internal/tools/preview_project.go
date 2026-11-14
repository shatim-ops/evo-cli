// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// namedProject is the handler of a tool whose preview names the project
// the write lands in: a write in the wrong project is the costliest
// mistake of a change, and the project comes from the profile, the
// directory, the key or the resource, not from the call (release plan 1.6,
// «Превью create называет проект», coverage-1.5.md П6; release plan 1.9,
// «Превью называет проект записи»). One point for every such tool, not a
// line in each handler.
//
// A call whose request carries the project (a create of Decl.Created) names
// the call's: the project_id argument, else the configuration's. A preview
// that names it already (engine.CreatePreview: project_id with
// project_from) is left as it is. Where the project comes from
// (project_from) waits for the human lines of the CLI (release plan 1.6,
// «Линия CLI»): the human preview does not show it yet (humanUnnamed, С2).
//
// A call that addresses a resource by id and sends no project of its own
// (idArg.target: a delete, a change, a create inside a parent) writes
// where the resource is, and a key of the organization reaches a resource
// of any project: its preview names the project of the resource, read
// before the question (ownerKind.project), with a note when that is not
// the call's project. Where the project cannot be read, the preview says
// so and names none — never the call's project for a resource that may be
// another's.
func (r *Registry) namedProject(t Tool, next Handler) Handler {
	targets := targetArgs(t.Name)
	return func(ctx context.Context, args map[string]any) (string, bool) {
		text, isErr := next(ctx, args)
		var m map[string]any
		if !isErr || json.Unmarshal([]byte(text), &m) != nil || m["status"] != "confirmation_required" {
			return text, isErr
		}
		// Into the preview object where there is one (engine.CreatePreview:
		// the CLI shows its project line), else beside the message, where
		// the CLI shows every field.
		at := m
		if p, ok := m["preview"].(map[string]any); ok {
			at = p
		}
		for _, a := range targets {
			ids := a.values(args)
			if len(ids) == 0 {
				continue
			}
			project, note := r.targetProject(ctx, t, a, ids[0], args)
			delete(at, "project_id")
			delete(at, "project_from")
			if project != "" {
				at["project_id"], at["project_from"] = project, engine.OriginResource
			}
			if note != "" {
				at["project_note"] = note
			}
			return toJSON(m), isErr
		}
		if namesProject(m) {
			return text, isErr
		}
		project := r.callProject(args)
		if project == "" {
			return text, isErr
		}
		at["project_id"] = project
		return toJSON(m), isErr
	}
}

// callProject is the project of the call: the project_id argument, else
// the configuration's — no request.
func (r *Registry) callProject(args map[string]any) string {
	if p := stringArg(args, "project_id"); p != "" {
		return p
	}
	eng, err := r.newEngine(nil)
	if err != nil || eng == nil {
		return ""
	}
	return eng.Config.ProjectID
}

// targetProject is the project of the resource id that argument a of tool
// t names, and the note of a preview on it: empty when the resource is the
// call's project's; that the write goes to another project; or, with no
// project, why it is not determined.
func (r *Registry) targetProject(ctx context.Context, t Tool, a idArg, id string, args map[string]any) (project, note string) {
	project, err := a.kind.project(ctx, &ownerCall{r: r, args: args, group: t.Group}, id)
	switch {
	case err != nil:
		return "", i18n.Tf("not determined: reading the project of %s %s failed; the change goes to the project of that resource. Confirm only if you know whose resource it is; if the task names no project, ask the person",
			"не определён: прочитать проект ресурса «%s» %s не удалось; изменение уйдёт в проект этого ресурса. Подтверждайте, только если знаете, чей это ресурс; если проект не назван в задаче — спросите человека", a.kind.what, id)
	case project == "":
		return "", i18n.Tf("not determined: the call's project does not hold %s %s, and its API names no project; the change goes to the project of that resource. Confirm only if you know whose resource it is; if the task names no project, ask the person",
			"не определён: ресурса «%s» %s нет в проекте вызова, а его API проект не называет; изменение уйдёт в проект этого ресурса. Подтверждайте, только если знаете, чей это ресурс; если проект не назван в задаче — спросите человека", a.kind.what, id)
	case project != r.callProject(args):
		return project, i18n.Tf("%s %s is in another project, not the call's; the change goes there. Confirm only if that is the project you mean; if the task names no project, ask the person",
			"ресурс «%s» %s — в другом проекте, не в проекте вызова; изменение уйдёт туда. Подтверждайте, только если это нужный проект; если проект не назван в задаче — спросите человека", a.kind.what, id)
	}
	return project, ""
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
