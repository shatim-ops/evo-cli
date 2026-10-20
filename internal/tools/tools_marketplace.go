// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/marketplace"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
)

// marketplace_list: the read-only showcase of the VM images of the shared
// catalogue and the Managed Kubernetes addons, in one list. Deploying stays
// in the trees (provision with image, k8s_add_addon with name): no unified
// install tool (PR #65).

const marketplaceListDescription = "Read-only showcase of what Cloud.ru offers to deploy, in one list: VM images of the shared catalogue (OS images and Cloud.ru Marketplace products; the project's own images are not included) and Managed Kubernetes addons. Changes nothing. " +
	"kind: vm-os (type public, or a marketplace image whose name or display name starts with an OS family word: Ubuntu, Debian, CentOS, Rocky, Alma, Fedora, openSUSE, Astra, Alt/Альт, RED OS/РЕД ОС, SberLinux, ROSA/РОСА, Windows — a heuristic, the catalogue has no such field), vm-app (any other marketplace image, e.g. nginx-ubuntu-22.04), k8s-addon. " +
	"query matches a substring of the name or display_name (addons: the description too), case-insensitive. Images the project cannot use and images ready in no zone are hidden unless all=true (counted in hidden_not_in_project, hidden_not_ready). " +
	"Rows {kind, name, display_name, id (images), billable (addons only: whether an image or its licence is paid is not known — the price calculation gives 0 for every image), available_in_project (images, when checked against the catalogue as the project sees it), ready=false (an image ready in no zone, with all=true), deploy{cli, tool, args}}: deploy is provision with args.image (the name, or the UUID when names repeat) or, for an addon, k8s_add_addon with args.name (plus cluster_id; k8s_addon_versions shows its versions); an image not available to the project has no deploy. " +
	"Sorted by kind, then display name; counts{vm-os, vm-app, k8s-addon} are the rows shown per kind, total the matches before hiding. " +
	"If one catalogue cannot be read, the other is still answered, with warnings[]; an error only when none is read."

func marketplaceListSchema() map[string]any {
	kind := strProp("Only this kind: vm-os (OS images), vm-app (Marketplace products on a VM) or k8s-addon (Managed Kubernetes addons). Default: all three; a VM kind does not read the addon catalogue and k8s-addon not the image one.")
	kind["enum"] = marketplace.Kinds
	return objSchema(map[string]any{
		"query": strProp("Substring of the name or display name (addons: also the description), case-insensitive, e.g. 'astra', 'nginx', 'cert'. Default: everything."),
		"kind":  kind,
		"all":   boolProp("Also show the images hidden by default: those the project cannot use (available_in_project=false, no deploy) and those ready in no zone (ready=false)."),
	})
}

// marketRow is one row of marketplace_list.
type marketRow struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	ID          string `json:"id,omitempty"`
	// Billable is the addon catalogue's flag; absent for an image (unknown).
	Billable *bool `json:"billable,omitempty"`
	// AvailableInProject is set for an image when the check was made (or
	// the image is unavailable); absent otherwise and for an addon.
	AvailableInProject *bool `json:"available_in_project,omitempty"`
	// Ready is false for an image ready in no zone (shown with all=true).
	Ready  *bool          `json:"ready,omitempty"`
	Deploy map[string]any `json:"deploy,omitempty"`
}

func marketRowOf(it marketplace.Item, checked bool) marketRow {
	row := marketRow{Kind: it.Kind, Name: it.Name, DisplayName: it.DisplayName}
	if a := it.Addon; a != nil {
		billable := a.IsBillable
		row.Billable = &billable
		row.Deploy = map[string]any{
			"cli":  "evo k8s add-addon <cluster-id> " + images.ShellQuote(a.Name),
			"tool": "k8s_add_addon",
			"args": map[string]any{"name": a.Name},
		}
		return row
	}
	img := it.Image
	row.ID = img.ID
	switch {
	case img.NotInProject:
		row.AvailableInProject = new(bool)
	case checked:
		yes := true
		row.AvailableInProject = &yes
	}
	if !img.NotInProject && !img.Usable() {
		row.Ready = new(bool)
	}
	if !img.NotInProject {
		row.Deploy = map[string]any{
			"cli":  "evo vm create <vm-name> --image " + images.ShellQuote(it.ImageRef),
			"tool": "provision",
			"args": map[string]any{"image": it.ImageRef},
		}
	}
	return row
}

// handleMarketplaceList merges the image catalogue and the addon catalogue.
// Read-only.
func (s *Registry) handleMarketplaceList(ctx context.Context, args map[string]any) (string, bool) {
	opt := marketplace.Options{
		Query: stringArg(args, "query"),
		Kind:  strings.ToLower(strings.TrimSpace(stringArg(args, "kind"))),
		All:   boolArg(args, "all", false),
	}
	if !marketplace.ValidKind(opt.Kind) {
		return usageT("'kind' must be one of %s, got %q", "'kind' должен быть одним из: %s, передано %q", strings.Join(marketplace.Kinds, ", "), opt.Kind), true
	}
	eng, err := s.newEngine(nil)
	if err != nil {
		return errResult(err), true
	}
	if eng == nil {
		return errResult(fmt.Errorf("engine unavailable")), true
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return errResult(err), true
	}
	eng.Cloud.SetToken(tok.Value)
	pid := eng.Config.ProjectID
	res, err := marketplace.List(ctx, marketplace.Sources{
		Images: &images.Catalog{Cloud: eng.Cloud, ProjectID: pid},
		Addons: mk8s.New(tok.Value, pid, ""),
	}, opt)
	if err != nil {
		return errResult(err), true
	}
	rows := make([]marketRow, 0, len(res.Items))
	flagged := false
	for _, it := range res.Items {
		rows = append(rows, marketRowOf(it, res.InProjectChecked))
		flagged = flagged || (it.Image != nil && it.Image.NotInProject)
	}
	out := map[string]any{
		"items":  rows,
		"shown":  len(rows),
		"total":  res.Total,
		"counts": res.Counts,
		"hidden": res.Hidden(),
	}
	if res.HiddenNotInProject > 0 {
		out["hidden_not_in_project"] = res.HiddenNotInProject
	}
	if res.HiddenNotReady > 0 {
		out["hidden_not_ready"] = res.HiddenNotReady
	}
	if flagged || res.HiddenNotInProject > 0 {
		out["note"] = notInProjectNote
	}
	if len(res.Warnings) > 0 {
		out["warnings"] = res.Warnings
	}
	return toJSON(out), false
}
