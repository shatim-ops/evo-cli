// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"

	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/marketplace"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
)

// marketplace_list: the read-only showcase of the VM images of the shared
// catalogue and the Managed Kubernetes addons, in one list. Deploying stays
// in the trees (provision with image, k8s_add_addon with name): no unified
// install tool (PR #65).

const marketplaceListDescription = "Read-only: what Cloud.ru offers to deploy, in one list: shared-catalogue VM images and Managed Kubernetes addons. " +
	"vm-os is a heuristic: public images and marketplace images named after an OS family. " +
	"Images the project cannot use or ready in no zone are hidden unless all=true. " +
	"A row's deploy{cli, tool, args} is provision with args.image, or k8s_add_addon with args.name plus cluster_id; none for an image the project cannot use. " +
	"billable: addons only (an image's price is unknown). If one catalogue cannot be read, the other comes with warnings[]."

func marketplaceListSchema() map[string]any {
	// As enumProp lists them, so checkedArgs refuses others (G13-1).
	kind := enumProp("Only this kind: vm-os (OS images), vm-app (Marketplace products on a VM) or k8s-addon. Default: all three.", marketplace.Kinds...)
	return objSchema(map[string]any{
		"query": strProp("Substring of the name or display name (addons: also the description), case-insensitive. Default: everything."),
		"kind":  kind,
		"all":   boolProp("Also show the hidden images (available_in_project=false or ready=false)."),
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
		Kind:  stringArg(args, "kind"),
		All:   boolArg(args, "all", false),
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
