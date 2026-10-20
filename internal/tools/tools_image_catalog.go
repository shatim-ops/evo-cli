// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"sort"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// imageListSchema is the schema of image_list and of its alias list_images.
func imageListSchema() map[string]any {
	typ := strProp("Only this type: public (OS images), marketplace (products of the Cloud.ru Marketplace) or private (images of the project). Default: all three.")
	typ["enum"] = images.Types
	return objSchema(map[string]any{
		"query": strProp("Substring of the image name or display name, case-insensitive (e.g. 'astra', 'ubuntu'). Default: the whole catalogue."),
		"type":  typ,
		"zone":  strProp("Only images ready in this availability zone: name ('ru.AZ-1', 'AZ-1') or UUID."),
		"all":   boolProp("Also show catalogue images ready in no zone and those the project cannot use (hidden by default: the shared catalogue carries test and broken images, and images a VM of this project cannot be created from — these come with available_in_project=false)."),
	})
}

// imageRow is one compact row of image_list: what an agent needs to pick an
// image, without the icon, the cloud-init template or the zone objects.
type imageRow struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name,omitempty"`
	Type        string   `json:"type,omitempty"`
	ZonesReady  []string `json:"zones_ready"`
	MinDiskGB   int      `json:"min_disk_gb,omitempty"`
	// AvailableInProject is false for an image of the shared catalogue the
	// project cannot create a VM from (the API answers 422 wrong_image);
	// absent otherwise.
	AvailableInProject *bool `json:"available_in_project,omitempty"`
}

// zoneNamer names zones for a row: the image's own zone name when it has
// one, else the name from the zone list, read once and only when needed.
type zoneNamer struct {
	ctx    context.Context
	eng    *engine.Engine
	byID   map[string]string
	loaded bool
	zones  []*provision.AvailabilityZone
	err    error
}

func (z *zoneNamer) load() {
	if z.loaded {
		return
	}
	z.loaded = true
	z.zones, z.err = z.eng.Cloud.ListAvailabilityZones(z.ctx)
	z.byID = map[string]string{}
	for _, zone := range z.zones {
		z.byID[zone.ID] = zone.Name
	}
}

func (z *zoneNamer) short(az provision.ImageAvailZone) string {
	name := az.AvailabilityZoneName
	if name == "" && az.AvailabilityZoneID != "" {
		z.load()
		name = z.byID[az.AvailabilityZoneID]
	}
	if name == "" {
		return az.AvailabilityZoneID
	}
	return images.ZoneShort(name)
}

func (z *zoneNamer) readyShort(zones []provision.ImageAvailZone) []string {
	out := []string{}
	for _, az := range zones {
		if provision.ZoneReady(az) {
			out = append(out, z.short(az))
		}
	}
	sort.Strings(out)
	return out
}

// zoneID resolves a zone given by name ("ru.AZ-1"), short name ("AZ-1") or
// UUID.
func (z *zoneNamer) zoneID(ref string) (string, error) {
	if looksUUID(ref) {
		return ref, nil
	}
	z.load()
	if z.err != nil {
		return "", z.err
	}
	var known []string
	for _, zone := range z.zones {
		if strings.EqualFold(zone.Name, ref) || strings.EqualFold(images.ZoneShort(zone.Name), ref) {
			return zone.ID, nil
		}
		known = append(known, zone.Name)
	}
	return "", clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("zone %q not found", "зона %q не найдена", ref),
		i18n.Tf("Zones: %s (evo vm zones).", "Зоны: %s (evo vm zones).", strings.Join(known, ", ")))
}

// handleImageList searches the image catalogue (list_images is the same
// handler). Read-only.
func (s *Registry) handleImageList(ctx context.Context, args map[string]any) (string, bool) {
	f := images.Filter{Type: strings.ToLower(strings.TrimSpace(stringArg(args, "type"))), All: boolArg(args, "all", false)}
	if !images.ValidType(f.Type) {
		return usageT("'type' must be one of %s, got %q", "'type' должен быть одним из: %s, передано %q", strings.Join(images.Types, ", "), f.Type), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	zn := &zoneNamer{ctx: ctx, eng: eng}
	if ref := strings.TrimSpace(stringArg(args, "zone")); ref != "" {
		if f.ZoneID, err = zn.zoneID(ref); err != nil {
			return errResult(err), true
		}
	}
	cat := images.Catalog{Cloud: eng.Cloud, ProjectID: eng.Config.ProjectID}
	res, err := cat.Search(ctx, stringArg(args, "query"), f)
	if err != nil {
		return errResult(err), true
	}
	rows := make([]imageRow, 0, len(res.Images))
	for _, img := range res.Images {
		row := imageRow{
			ID: img.ID, Name: img.Name, DisplayName: img.DisplayName, Type: img.Type,
			ZonesReady: zn.readyShort(img.AvailZones), MinDiskGB: img.MinDisk,
		}
		if img.NotInProject {
			row.AvailableInProject = new(bool)
		}
		rows = append(rows, row)
	}
	out := map[string]any{"images": rows, "shown": len(rows), "total": res.Total, "hidden": res.Hidden}
	if res.HiddenNotInProject > 0 {
		out["hidden_not_in_project"] = res.HiddenNotInProject
	}
	if res.HiddenNotInProject > 0 || hasNotInProject(res.Images) {
		out["note"] = notInProjectNote
	}
	if res.Warning != "" {
		out["warning"] = res.Warning
	}
	return toJSON(out), false
}

// notInProjectNote explains available_in_project=false and
// hidden_not_in_project to an agent.
const notInProjectNote = "Some images of the shared Cloud.ru catalogue are not available to this project (the catalogue as the project sees it, project_id=, leaves them out): a VM cannot be created from them — provision, deploy and deploy_plan refuse them before anything is created (the API would answer 422 wrong_image). Why is unverified: probably the Marketplace product has to be connected to the project first."

func hasNotInProject(list []*provision.Image) bool {
	for _, img := range list {
		if img.NotInProject {
			return true
		}
	}
	return false
}

// imageErrResult is errResult plus the candidates of an ambiguous
// reference, for an agent to pick one or ask.
func imageErrResult(err error) string {
	var amb *images.AmbiguousError
	if !stderrors.As(err, &amb) {
		return errResult(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(errResult(err)), &m)
	cands := make([]map[string]any, 0, len(amb.Candidates))
	for _, c := range amb.Candidates {
		cand := map[string]any{"id": c.ID, "name": c.Name, "display_name": c.DisplayName, "type": c.Type}
		if c.NotInProject {
			cand["available_in_project"] = false
		}
		cands = append(cands, cand)
	}
	m["candidates"] = cands
	return toJSON(m)
}

// handleImageGet returns one image by name or UUID: a card with what evo
// fills in its metadata fields and what the person must, and the command
// that creates a VM from it. The icon, the cloud-init template and the raw
// metadata fields only with full=true: they are kilobytes of an agent's
// context.
func (s *Registry) handleImageGet(ctx context.Context, args map[string]any) (string, bool) {
	imageArg, imageIDArg := strings.TrimSpace(stringArg(args, "image")), strings.TrimSpace(stringArg(args, "image_id"))
	if imageArg != "" && imageIDArg != "" {
		// Both: the image was taken and image_id dropped without a word
		// (QA 20.10, QA-11).
		return errResult(clierrors.New(clierrors.CodeUsage,
			i18n.T("pass image or image_id, not both", "укажите image или image_id, не оба"),
			i18n.T("image takes a name or a UUID; image_id is the same as image with a UUID.",
				"image — имя или UUID образа; image_id — то же, что image с UUID."))), true
	}
	ref := firstNonEmptyStr(imageArg, imageIDArg)
	if ref == "" {
		return usageT("'image' (name or UUID) or 'image_id' is required", "нужен 'image' (имя или UUID) или 'image_id'"), true
	}
	full := boolArg(args, "full", false)
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	cat := images.Catalog{Cloud: eng.Cloud, ProjectID: eng.Config.ProjectID}
	img, err := cat.Get(ctx, ref)
	if err != nil {
		return imageErrResult(err), true
	}

	var card map[string]any
	b, _ := json.Marshal(img)
	_ = json.Unmarshal(b, &card)
	if !full {
		delete(card, "icon")
		delete(card, "user_data_template")
		// fields[] below carries the metadata fields, without the defaults
		// of secret ones.
		delete(card, "image_metadata")
	}

	zn := &zoneNamer{ctx: ctx, eng: eng}
	fields := images.Fields(img.MetadataFields)
	needs := images.NeedsUser(fields)
	if needs == nil {
		needs = []string{}
	}
	out := map[string]any{
		"image":           card,
		"ready":           img.Ready(),
		"zone_states":     zoneStates(img),
		"zones_ready":     zn.readyShort(img.AvailZones),
		"fields":          fields,
		"fields_required": needs,
		"create": map[string]any{
			"cli":  "evo vm create <vm-name> --image " + images.ShellQuote(img.Name) + metaFlags(needs),
			"tool": "provision",
			"args": map[string]any{"image_id": img.ID},
		},
	}
	if len(needs) > 0 {
		out["note"] = "The image has mandatory fields evo cannot fill (" + strings.Join(needs, ", ") +
			"): pass them to provision in image_metadata (CLI: --meta field=value), or the call is refused before anything is created."
	}
	if img.NotInProject {
		// No "create": provision would refuse it.
		delete(out, "create")
		out["available_in_project"] = false
		out["note"] = notInProjectNote + " Pick another image: image_list (it hides such images by default)."
	}
	return toJSON(out), false
}

// metaFlags are the --meta flags of the fields the person must fill.
func metaFlags(fields []string) string {
	out := ""
	for _, f := range fields {
		out += " --meta " + f + "=<value>"
	}
	return out
}

// handleListImages is list_images as it was before image_list: the full
// image objects of the shared catalogue plus the project's own, no
// arguments. Its answer is part of the MCP contract, so it stays; new
// callers should use image_list.
func (s *Registry) handleListImages(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return errResult(err), true
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return errResult(err), true
	}
	eng.Cloud.SetToken(tok.Value)

	images, err := eng.Cloud.ListImages(ctx)
	if err != nil {
		return errResult(err), true
	}
	// The shared catalogue does not contain images built inside the project, so
	// they are fetched separately and merged — otherwise a freshly built image
	// is missing from the very list used to find it.
	seen := make(map[string]bool, len(images))
	for _, img := range images {
		seen[img.ID] = true
	}
	own, ownErr := eng.Cloud.ListImagesInProject(ctx, eng.Config.ProjectID)
	for _, img := range own {
		if !seen[img.ID] {
			images = append(images, img)
		}
	}
	res := map[string]any{"images": images}
	if ownErr != nil {
		res["warning"] = "не удалось получить образы проекта: " + ownErr.Error()
	}
	return toJSON(res), false
}
