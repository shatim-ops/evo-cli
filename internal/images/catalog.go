// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"context"
	stderrors "errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Cloud is the part of the compute API the catalogue reads.
type Cloud interface {
	ListImages(ctx context.Context, filter ...provision.ImageFilter) ([]*provision.Image, error)
	ListImagesInProject(ctx context.Context, projectID string) ([]*provision.Image, error)
	GetImage(ctx context.Context, id string) (*provision.ImageDetail, error)
}

// Catalog is the image catalogue as one project sees it: the shared images
// (public and marketplace) and the project's own (private).
type Catalog struct {
	Cloud Cloud
	// ProjectID scopes the private images; empty leaves them out.
	ProjectID string
}

// Filter narrows a search beyond the query.
type Filter struct {
	// Type is public, marketplace, private or "" for all three.
	Type string
	// ZoneID keeps the images ready in that zone (present there, with
	// All).
	ZoneID string
	// All shows the catalogue images ready in no zone and those the
	// project cannot use (NotInProject) too. They are hidden by default:
	// the shared catalogue carries test and broken images, and images a
	// VM of the project cannot be created from (BACKLOG [images]). The
	// project's own images are always shown: one being built is what its
	// owner looks for.
	All bool
	// Shared leaves the project's own images out: the shared catalogue
	// only, public and marketplace (evo marketplace list). The project's
	// list is not read at all.
	Shared bool
}

// SearchResult is a search answer.
type SearchResult struct {
	// Images are the images to show, sorted: public, marketplace, private,
	// then by display name.
	Images []*provision.Image
	// Total is how many images matched the query and the filter before
	// the unavailable ones were hidden.
	Total int
	// Hidden is how many of them were hidden (Total - len(Images)).
	Hidden int
	// HiddenNotInProject is how many of the hidden ones the project cannot
	// use (NotInProject); the rest are ready in no zone.
	HiddenNotInProject int
	// Warning says what could not be read (the project's images, the
	// catalogue as the project sees it) when the rest still was.
	Warning string
}

// Types are the accepted values of Filter.Type.
var Types = []string{provision.ImageTypePublic, provision.ImageTypeMarketplace, provision.ImageTypePrivate}

// ValidType reports whether t is "" or one of Types.
func ValidType(t string) bool {
	if t == "" {
		return true
	}
	for _, x := range Types {
		if t == x {
			return true
		}
	}
	return false
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsUUID reports whether ref is an image UUID rather than a name.
func IsUUID(ref string) bool { return uuidRe.MatchString(ref) }

// fetch reads the images that may match: the shared catalogue through the
// server-side search (name= and display_name= — two calls, merged by id),
// the project's images through their own list, filtered here (that list
// has no search). With a project, the same search runs once more as the
// project sees the catalogue (project_id=): a shared image missing from
// that answer is marked NotInProject. If that search fails, nothing is
// marked and the warning says so.
func (c Catalog) fetch(ctx context.Context, query string, f Filter) ([]*provision.Image, string, error) {
	var out []*provision.Image
	seen := map[string]bool{}
	add := func(list []*provision.Image, private bool) {
		for _, img := range list {
			if img == nil || seen[img.ID] {
				continue
			}
			seen[img.ID] = true
			if private && img.Type == "" {
				img.Type = provision.ImageTypePrivate
			}
			out = append(out, img)
		}
	}
	var warnings []string
	if f.Type != provision.ImageTypePrivate {
		base := provision.ImageFilter{Type: f.Type, AvailabilityZoneID: f.ZoneID}
		list, err := c.search(ctx, query, base)
		if err != nil {
			return nil, "", err
		}
		add(list, false)
		if c.ProjectID != "" {
			base.ProjectID = c.ProjectID
			inProject, err := c.search(ctx, query, base)
			if err != nil {
				warnings = append(warnings, i18n.Tf("the catalogue as the project sees it was not read, so images the project cannot use are not marked: %v",
					"каталог, каким его видит проект, не прочитан — недоступные проекту образы не отмечены: %v", err))
			} else {
				visible := make(map[string]bool, len(inProject))
				for _, img := range inProject {
					if img != nil {
						visible[img.ID] = true
					}
				}
				for _, img := range list {
					if img != nil && !visible[img.ID] {
						img.NotInProject = true
					}
				}
			}
		}
	}
	if (f.Type == "" || f.Type == provision.ImageTypePrivate) && c.ProjectID != "" && !f.Shared {
		own, err := c.Cloud.ListImagesInProject(ctx, c.ProjectID)
		switch {
		case err != nil && f.Type == provision.ImageTypePrivate:
			return nil, "", err
		case err != nil:
			warnings = append(warnings, i18n.Tf("the project's own images were not read: %v", "образы проекта не прочитаны: %v", err))
		default:
			add(own, true)
		}
	}
	return out, strings.Join(warnings, "; "), nil
}

// search is the server-side search of one query: the whole list for "",
// else name= and display_name=, merged by id.
func (c Catalog) search(ctx context.Context, query string, base provision.ImageFilter) ([]*provision.Image, error) {
	if query == "" {
		return c.Cloud.ListImages(ctx, base)
	}
	byName, byDisplay := base, base
	byName.Name, byDisplay.DisplayName = query, query
	var out []*provision.Image
	seen := map[string]bool{}
	for _, q := range []provision.ImageFilter{byName, byDisplay} {
		list, err := c.Cloud.ListImages(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, img := range list {
			if img != nil && !seen[img.ID] {
				seen[img.ID] = true
				out = append(out, img)
			}
		}
	}
	return out, nil
}

// matches checks on this side what the server was asked: a substring of the
// name or the display name, case-insensitive, and the type. The server
// already filtered the shared catalogue; the project's list is not filtered
// at all.
func matches(img *provision.Image, query string, f Filter) bool {
	if query != "" {
		q := strings.ToLower(query)
		if !strings.Contains(strings.ToLower(img.Name), q) && !strings.Contains(strings.ToLower(img.DisplayName), q) {
			return false
		}
	}
	if f.Type != "" && img.Type != "" && img.Type != f.Type {
		return false
	}
	if f.Shared && img.Type == provision.ImageTypePrivate {
		return false
	}
	if f.ZoneID != "" && !inZone(img, f.ZoneID, !f.All) {
		return false
	}
	return true
}

func inZone(img *provision.Image, zoneID string, ready bool) bool {
	for _, az := range img.AvailZones {
		if az.AvailabilityZoneID == zoneID && (!ready || provision.ZoneReady(az)) {
			return true
		}
	}
	return false
}

// Visible reports whether an image is shown without --all: the project can
// use it (not NotInProject) and it is ready in some zone, or it is the
// project's own.
func Visible(img *provision.Image) bool {
	if img.NotInProject {
		return false
	}
	return img.Type == provision.ImageTypePrivate || img.Usable()
}

var typeOrder = map[string]int{provision.ImageTypePublic: 0, provision.ImageTypeMarketplace: 1, provision.ImageTypePrivate: 2}

// Title is the image's display name, or its name when it has none.
func Title(img *provision.Image) string {
	if strings.TrimSpace(img.DisplayName) != "" {
		return img.DisplayName
	}
	return img.Name
}

func sortImages(list []*provision.Image) {
	rank := func(t string) int {
		if r, ok := typeOrder[t]; ok {
			return r
		}
		return len(typeOrder)
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if ra, rb := rank(a.Type), rank(b.Type); ra != rb {
			return ra < rb
		}
		if ta, tb := strings.ToLower(Title(a)), strings.ToLower(Title(b)); ta != tb {
			return ta < tb
		}
		return a.Name < b.Name
	})
}

// Search finds images by a substring of the name or the display name
// (case-insensitive; "" lists everything) and the filter.
func (c Catalog) Search(ctx context.Context, query string, f Filter) (*SearchResult, error) {
	query = strings.TrimSpace(query)
	if !ValidType(f.Type) {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("unknown image type %q", "неизвестный тип образа %q", f.Type),
			i18n.Tf("Types: %s.", "Типы: %s.", strings.Join(Types, ", ")))
	}
	all, warning, err := c.fetch(ctx, query, f)
	if err != nil {
		return nil, err
	}
	res := &SearchResult{Warning: warning}
	for _, img := range all {
		if !matches(img, query, f) {
			continue
		}
		res.Total++
		switch {
		case f.All || Visible(img):
			res.Images = append(res.Images, img)
		case img.NotInProject:
			res.HiddenNotInProject++
		}
	}
	res.Hidden = res.Total - len(res.Images)
	sortImages(res.Images)
	return res, nil
}

// MaxCandidates is how many candidates an ambiguity names.
const MaxCandidates = 10

// AmbiguousError is a reference that matches several images. It carries
// the USAGE CLIError (errors.As finds it) and the candidates.
type AmbiguousError struct {
	Ref        string
	Candidates []*provision.Image
	err        *clierrors.CLIError
}

func (e *AmbiguousError) Error() string { return e.err.Error() }
func (e *AmbiguousError) Unwrap() error { return e.err }

// CandidateLine is one candidate as an ambiguity lists it.
func CandidateLine(img *provision.Image) string {
	line := fmt.Sprintf("%s — %s — %s", img.Name, Title(img), img.Type)
	if img.NotInProject {
		line += i18n.T(" — not available to the project", " — недоступен проекту")
	}
	return line
}

func ambiguous(ref string, cands []*provision.Image) error {
	sortImages(cands)
	lines := []string{i18n.Tf("image %q is ambiguous: %d matches", "образ %q неоднозначен: совпадений — %d", ref, len(cands))}
	for i, img := range cands {
		if i == MaxCandidates {
			lines = append(lines, i18n.Tf("  … and %d more", "  … и ещё %d", len(cands)-MaxCandidates))
			break
		}
		lines = append(lines, "  "+CandidateLine(img))
	}
	shown := cands
	if len(shown) > MaxCandidates {
		shown = shown[:MaxCandidates]
	}
	return &AmbiguousError{Ref: ref, Candidates: shown, err: clierrors.New(clierrors.CodeUsage,
		strings.Join(lines, "\n"),
		i18n.Tf("Give the exact name or the UUID; all matches: evo image list %s", "Укажите точное имя или UUID; все совпадения: evo image list %s", shellQuote(ref)))}
}

// NotFoundError is the refusal of an image the catalogue does not have, by
// the reference the caller gave (a name or a UUID).
func NotFoundError(ref string) error { return notFound(ref) }

func notFound(ref string) error {
	return clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("image %q not found", "образ %q не найден", ref),
		i18n.Tf("Find it: evo image list %s", "Найдите образ: evo image list %s", shellQuote(searchWord(ref))))
}

// searchWord is the part of a reference worth searching for when it found
// nothing: its first word.
func searchWord(ref string) string {
	if f := strings.Fields(ref); len(f) > 0 {
		return f[0]
	}
	return ref
}

// ShellQuote quotes s for a command line shown to a person, when it needs it.
func ShellQuote(s string) string { return shellQuote(s) }

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t'\"$`\\") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// FromDetail is the catalogue entry of an image read by UUID.
func FromDetail(d *provision.ImageDetail) *provision.Image {
	return &provision.Image{
		ID: d.ID, Name: d.Name, DisplayName: d.DisplayName, Description: d.Description,
		Type: d.Type, ProjectID: d.ProjectID, Public: d.Public,
		MinCPU: d.MinCPU, MinRAM: d.MinRAM, MinDisk: d.MinDisk, CreatedTime: d.CreatedTime,
		OS: d.OS, Status: d.Status, AvailZones: d.AvailZones, NotInProject: d.NotInProject,
	}
}

// Resolve finds one image by reference: a UUID; else an exact name
// (case-insensitive); else an exact display name; else the only image whose
// name or display name contains it. Several matches at the first step that
// has any are an ambiguity (USAGE, with up to MaxCandidates candidates);
// none at all is NOT_FOUND. There is no "family → latest version": the
// catalogue's names carry no version scheme to rely on.
func (c Catalog) Resolve(ctx context.Context, ref string) (*provision.Image, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.T("image is empty", "не задан образ"),
			i18n.T("Give a name, a display name or a UUID: evo image list", "Укажите имя, отображаемое имя или UUID: evo image list"))
	}
	if IsUUID(ref) {
		d, err := c.byID(ctx, ref)
		if err != nil {
			return nil, err
		}
		c.MarkInProject(ctx, d)
		return FromDetail(d), nil
	}
	res, err := c.Search(ctx, ref, Filter{All: true})
	if err != nil {
		return nil, err
	}
	steps := []func(*provision.Image) bool{
		func(img *provision.Image) bool { return strings.EqualFold(img.Name, ref) },
		func(img *provision.Image) bool { return strings.EqualFold(strings.TrimSpace(img.DisplayName), ref) },
	}
	for _, match := range steps {
		var hit []*provision.Image
		for _, img := range res.Images {
			if match(img) {
				hit = append(hit, img)
			}
		}
		switch len(hit) {
		case 0:
			continue
		case 1:
			return hit[0], nil
		default:
			return nil, ambiguous(ref, hit)
		}
	}
	// A substring: the images usable somewhere first, so a broken copy in
	// the catalogue, or one the project cannot use, does not make a
	// working one ambiguous.
	var usable []*provision.Image
	for _, img := range res.Images {
		if Visible(img) {
			usable = append(usable, img)
		}
	}
	cands := usable
	if len(cands) == 0 {
		cands = res.Images
	}
	switch len(cands) {
	case 0:
		return nil, notFound(ref)
	case 1:
		return cands[0], nil
	default:
		return nil, ambiguous(ref, cands)
	}
}

func (c Catalog) byID(ctx context.Context, id string) (*provision.ImageDetail, error) {
	d, err := c.Cloud.GetImage(ctx, id)
	if err != nil {
		var ce *clierrors.CLIError
		// The compute adapter answers a 404 on an image with NOT_FOUND
		// (VM_NOT_FOUND before QA-10; a fake may still say so).
		if stderrors.As(err, &ce) && (ce.Code == clierrors.CodeNotFound || ce.Code == clierrors.CodeVMNotFound) {
			return nil, notFound(id)
		}
		return nil, err
	}
	if d == nil || d.ID == "" {
		return nil, notFound(id)
	}
	return d, nil
}

// Get is the full image (fields, template, icon) for a reference Resolve
// accepts: one read for a UUID, a search and a read for a name.
func (c Catalog) Get(ctx context.Context, ref string) (*provision.ImageDetail, error) {
	ref = strings.TrimSpace(ref)
	if IsUUID(ref) {
		d, err := c.byID(ctx, ref)
		if err != nil {
			return nil, err
		}
		c.MarkInProject(ctx, d)
		return d, nil
	}
	img, err := c.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	d, err := c.byID(ctx, img.ID)
	if err != nil {
		return nil, err
	}
	// The project's list knows an image is private; its detail may not say.
	if d.Type == "" {
		d.Type = img.Type
	}
	d.NotInProject = img.NotInProject
	return d, nil
}

// MarkInProject checks an image read by UUID against the catalogue as the
// project sees it: the project-scoped list filtered by the image's name
// must hold its id, else NotInProject is set. The project's own images and
// a catalogue without a project are not checked; a failing call marks
// nothing (the create call then answers for itself).
func (c Catalog) MarkInProject(ctx context.Context, d *provision.ImageDetail) {
	if d == nil || d.ProjectID != "" || d.Type == provision.ImageTypePrivate {
		return
	}
	d.NotInProject = c.MissingFromProject(ctx, d.ID, d.Name)
}

// MissingFromProject reports whether the catalogue as the project sees it
// lacks a shared image: the project-scoped list filtered by the name was
// read and does not hold the id. No project, no name or a failing call is
// false: unknown is not a refusal.
func (c Catalog) MissingFromProject(ctx context.Context, id, name string) bool {
	if c.ProjectID == "" || name == "" {
		return false
	}
	list, err := c.Cloud.ListImages(ctx, provision.ImageFilter{ProjectID: c.ProjectID, Name: name})
	if err != nil {
		return false
	}
	for _, img := range list {
		if img != nil && img.ID == id {
			return false
		}
	}
	return true
}

// NotInProjectError refuses a VM from an image the project cannot use
// (NotInProject), before anything is created: the create call would answer
// 422 wrong_image. NOT_FOUND, as the API sees it.
func NotInProjectError(image string) error {
	return clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("image %q is in the Cloud.ru catalogue but not available to the project: the API will not create a VM from it",
			"образ %q есть в каталоге Cloud.ru, но недоступен проекту: создать из него ВМ API не даст", image),
		i18n.T("Images the project can use: evo image list <query> (the unavailable ones are hidden; asked for all, they are shown marked). A Marketplace product probably has to be connected to the project first (unverified).",
			"Образы, доступные проекту: evo image list <запрос> (недоступные скрыты; если запросить все, они видны с пометкой). Продукт Marketplace, вероятно, сначала нужно подключить к проекту (не проверено)."))
}

// ZoneShort is a zone name without its region prefix: "ru.AZ-1" → "AZ-1".
func ZoneShort(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 && i < len(name)-1 {
		return name[i+1:]
	}
	return name
}
