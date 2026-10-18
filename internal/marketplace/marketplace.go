// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package marketplace is the read-only showcase of what Cloud.ru offers to
// deploy: the VM images of the shared catalogue (operating systems and
// Marketplace products) and the Managed Kubernetes addons, in one list.
//
// It installs nothing. Each row names the command of its own tree that
// deploys it (evo vm create --image, evo k8s add-addon): one unified install
// command was decided against (PR #65).
package marketplace

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The kinds of a row.
const (
	// KindOS is an operating system image: type public, or a marketplace
	// image whose name or display name starts with an OS family word.
	KindOS = "vm-os"
	// KindApp is any other marketplace image: a product on a VM.
	KindApp = "vm-app"
	// KindAddon is a Managed Kubernetes addon.
	KindAddon = "k8s-addon"
)

// Kinds are the accepted values of Options.Kind, in the order of the list.
var Kinds = []string{KindOS, KindApp, KindAddon}

// ValidKind reports whether k is "" or one of Kinds.
func ValidKind(k string) bool {
	if k == "" {
		return true
	}
	for _, x := range Kinds {
		if k == x {
			return true
		}
	}
	return false
}

// AddonSource is the part of the mk8s client the showcase reads: the whole
// addon catalogue, every page.
type AddonSource interface {
	ListAddons(ctx context.Context) ([]mk8s.Addon, error)
}

// Sources are the two catalogues. A nil one is not read.
type Sources struct {
	Images *images.Catalog
	Addons AddonSource
}

// Options narrow the list.
type Options struct {
	// Query is a substring of the name or the display name (of an addon,
	// the description too), case-insensitive; "" is everything.
	Query string
	// Kind is one of Kinds or "" for all.
	Kind string
	// All shows the images hidden by default too: those the project cannot
	// use and those ready in no zone (as evo image list --all).
	All bool
}

// Item is one row.
type Item struct {
	Kind        string
	Name        string
	DisplayName string
	// Image is the catalogue entry of a vm-os or vm-app row.
	Image *provision.Image
	// Addon is the catalogue entry of a k8s-addon row.
	Addon *mk8s.Addon
	// ImageRef is what the deploy command names for an image: its name, or
	// its UUID when several images of the catalogue share the name (the
	// name would be ambiguous to evo vm create --image).
	ImageRef string
}

// Title is the display name, or the name when there is none.
func (it Item) Title() string {
	if strings.TrimSpace(it.DisplayName) != "" {
		return it.DisplayName
	}
	return it.Name
}

// Result is the showcase.
type Result struct {
	// Items are sorted by kind (Kinds order), then display name.
	Items []Item
	// Counts are the shown items per kind; every kind is present.
	Counts map[string]int
	// Total is how many matched the query and the kind before the images
	// were hidden.
	Total int
	// HiddenNotInProject and HiddenNotReady are the hidden images by
	// reason: the project cannot use them; they are ready in no zone.
	HiddenNotInProject int
	HiddenNotReady     int
	// InProjectChecked says the images were checked against the catalogue
	// as the project sees it: a row without NotInProject is available.
	// False without a project or when that read failed.
	InProjectChecked bool
	// Warnings say what could not be read when the rest still was: a whole
	// catalogue, or the project's view of the images.
	Warnings []string
}

// Hidden is the number of hidden images.
func (r *Result) Hidden() int { return r.HiddenNotInProject + r.HiddenNotReady }

// List reads the catalogues the kind needs, at the same time, and merges
// them. One catalogue that fails is a warning; it is an error only when no
// catalogue was read.
func List(ctx context.Context, src Sources, opt Options) (*Result, error) {
	opt.Query = strings.TrimSpace(opt.Query)
	if !ValidKind(opt.Kind) {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("unknown kind %q", "неизвестный вид %q", opt.Kind),
			i18n.Tf("Kinds: %s.", "Виды: %s.", strings.Join(Kinds, ", ")))
	}
	wantImages := src.Images != nil && opt.Kind != KindAddon
	wantAddons := src.Addons != nil && (opt.Kind == "" || opt.Kind == KindAddon)

	var (
		wg       sync.WaitGroup
		imgRes   *images.SearchResult
		imgErr   error
		addons   []mk8s.Addon
		addonErr error
	)
	if wantImages {
		wg.Add(1)
		go func() {
			defer wg.Done()
			imgRes, imgErr = src.Images.Search(ctx, opt.Query, images.Filter{Shared: true, All: true})
		}()
	}
	if wantAddons {
		wg.Add(1)
		go func() {
			defer wg.Done()
			addons, addonErr = src.Addons.ListAddons(ctx)
		}()
	}
	wg.Wait()

	res := &Result{Counts: map[string]int{}}
	for _, k := range Kinds {
		res.Counts[k] = 0
	}
	read := 0
	var errs []error
	if wantImages {
		if imgErr != nil {
			errs = append(errs, fmt.Errorf(i18n.T("the VM image catalogue was not read: %w", "каталог образов ВМ не прочитан: %w"), imgErr))
		} else {
			read++
			res.addImages(imgRes, opt, src.Images.ProjectID != "")
		}
	}
	if wantAddons {
		if addonErr != nil {
			errs = append(errs, fmt.Errorf(i18n.T("the Managed Kubernetes addon catalogue was not read: %w", "каталог плагинов Managed Kubernetes не прочитан: %w"), addonErr))
		} else {
			read++
			res.addAddons(addons, opt)
		}
	}
	if read == 0 && len(errs) > 0 {
		if len(errs) == 1 {
			return nil, errs[0]
		}
		// The first error's code (errors.As) gives the exit code.
		return nil, fmt.Errorf("%w; %w", errs[0], errs[1])
	}
	for _, e := range errs {
		res.Warnings = append(res.Warnings, e.Error())
	}
	sortItems(res.Items)
	for _, it := range res.Items {
		res.Counts[it.Kind]++
	}
	return res, nil
}

func (res *Result) addImages(sr *images.SearchResult, opt Options, project bool) {
	if sr.Warning != "" {
		res.Warnings = append(res.Warnings, sr.Warning)
	}
	res.InProjectChecked = project && sr.Warning == ""
	names := map[string]int{}
	for _, img := range sr.Images {
		names[strings.ToLower(img.Name)]++
	}
	for _, img := range sr.Images {
		kind := ImageKind(img)
		if opt.Kind != "" && kind != opt.Kind {
			continue
		}
		res.Total++
		if !opt.All && !images.Visible(img) {
			if img.NotInProject {
				res.HiddenNotInProject++
			} else {
				res.HiddenNotReady++
			}
			continue
		}
		ref := img.Name
		if names[strings.ToLower(img.Name)] > 1 || ref == "" {
			ref = img.ID
		}
		res.Items = append(res.Items, Item{Kind: kind, Name: img.Name, DisplayName: img.DisplayName, Image: img, ImageRef: ref})
	}
}

func (res *Result) addAddons(list []mk8s.Addon, opt Options) {
	q := strings.ToLower(opt.Query)
	for i := range list {
		a := &list[i]
		if q != "" && !strings.Contains(strings.ToLower(a.Name+"\n"+a.DisplayName+"\n"+a.Description), q) {
			continue
		}
		res.Total++
		res.Items = append(res.Items, Item{Kind: KindAddon, Name: a.Name, DisplayName: a.DisplayName, Addon: a})
	}
}

func sortItems(list []Item) {
	rank := map[string]int{}
	for i, k := range Kinds {
		rank[k] = i
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if ra, rb := rank[a.Kind], rank[b.Kind]; ra != rb {
			return ra < rb
		}
		if ta, tb := strings.ToLower(a.Title()), strings.ToLower(b.Title()); ta != tb {
			return ta < tb
		}
		return a.Name < b.Name
	})
}

// ImageKind is vm-os for an OS image, vm-app for a product. A heuristic: the
// catalogue has no such field. Type public is an OS; a marketplace image
// (or one of no stated type) is an OS when its name or display name, trimmed,
// starts with an OS family word (OSFamily), else a product:
// AstraLinuxSmolensk1.8.3KES is an OS, nginx-ubuntu-22.04 a product built on
// one.
func ImageKind(img *provision.Image) string {
	if img.Type == provision.ImageTypePublic {
		return KindOS
	}
	if OSFamily(img.Name) != "" || OSFamily(img.DisplayName) != "" {
		return KindOS
	}
	return KindApp
}

// osFamilies are the OS family words a name may start with. A word marked
// whole must end there (end, a non-letter, or a capital after a small
// letter: "AltServer"), so "Altair" or "Rosatom" are not Alt or ROSA; the
// others are long enough to match as a prefix ("AstraLinuxSmolensk").
var osFamilies = []struct {
	family string
	re     *regexp.Regexp
	whole  bool
}{
	{"Ubuntu", regexp.MustCompile(`(?i)^ubuntu`), false},
	{"Debian", regexp.MustCompile(`(?i)^debian`), false},
	{"CentOS", regexp.MustCompile(`(?i)^centos`), false},
	{"Rocky Linux", regexp.MustCompile(`(?i)^rocky`), false},
	{"AlmaLinux", regexp.MustCompile(`(?i)^alma[ _-]?linux`), false},
	{"AlmaLinux", regexp.MustCompile(`(?i)^alma`), true},
	{"Fedora", regexp.MustCompile(`(?i)^fedora`), false},
	{"openSUSE", regexp.MustCompile(`(?i)^open[ _-]?suse`), false},
	{"Astra Linux", regexp.MustCompile(`(?i)^astra`), false},
	{"Alt Linux", regexp.MustCompile(`(?i)^alt[ _-]?linux`), false},
	{"Alt Linux", regexp.MustCompile(`(?i)^(alt|альт)`), true},
	{"RED OS", regexp.MustCompile(`(?i)^(red[ _-]?os|ред[ _-]?ос)`), true},
	{"SberLinux", regexp.MustCompile(`(?i)^sber[ _-]?linux`), false},
	{"ROSA", regexp.MustCompile(`(?i)^(rosa|роса)`), true},
	{"Windows", regexp.MustCompile(`(?i)^windows`), false},
}

// OSFamily names the OS family a name starts with ("Astra Linux", "RED OS",
// "Windows"…), or "" for none. Leading spaces are trimmed.
func OSFamily(name string) string {
	s := strings.TrimSpace(name)
	for _, f := range osFamilies {
		loc := f.re.FindStringIndex(s)
		if loc == nil {
			continue
		}
		if !f.whole || wordEnds(s, loc[1]) {
			return f.family
		}
	}
	return ""
}

// wordEnds reports whether a word of s ends at byte i.
func wordEnds(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(s[i:])
	if !unicode.IsLetter(next) {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:i])
	return unicode.IsUpper(next) && unicode.IsLower(prev)
}
