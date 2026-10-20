// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Versions of addon releases and the cluster requirements they carry.
// The API does not order releases (api-probe #5: traefik ascending, the
// others descending), so "latest" is picked here by semver.

// Support modes of a release, without the SUPPORT_MODE_ prefix the live API
// adds (the spec lists them bare).
const (
	ModeActive       = "ACTIVE"
	ModeExperimental = "EXPERIMENTAL"
	ModeEndOfSupport = "END_OF_SUPPORT"
	ModeEndOfLife    = "END_OF_LIFE"
	ModeInactive     = "INACTIVE"
)

// NormalizeMode is a release mode without its prefix, upper case:
// "SUPPORT_MODE_ACTIVE" and "active" are both "ACTIVE".
func NormalizeMode(m string) string {
	m = strings.ToUpper(strings.TrimSpace(m))
	return strings.TrimPrefix(m, "SUPPORT_MODE_")
}

// semver is a version split for comparison; ok is false for a string that
// is not one (it then sorts after every real version, by text).
type semver struct {
	nums  [3]int
	parts int // how many of nums were given: "1.26" → 2
	pre   []string
	ok    bool
}

func parseSemver(s string) semver {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	s, _, _ = strings.Cut(s, "+")
	var v semver
	core, pre, hasPre := strings.Cut(s, "-")
	if hasPre {
		v.pre = strings.Split(pre, ".")
	}
	fields := strings.Split(core, ".")
	if core == "" || len(fields) > 3 {
		return semver{}
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return semver{}
		}
		v.nums[i] = n
	}
	v.parts = len(fields)
	v.ok = true
	return v
}

// cmpNums compares the numeric parts only.
func cmpNums(a, b semver) int {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			if a.nums[i] < b.nums[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// compareSemver orders two parsed versions by semver precedence.
func compareSemver(a, b semver) int {
	if c := cmpNums(a, b); c != 0 {
		return c
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil:
			if nx != ny {
				if nx < ny {
					return -1
				}
				return 1
			}
		case ex == nil:
			return -1
		case ey == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(a.pre) < len(b.pre):
		return -1
	case len(a.pre) > len(b.pre):
		return 1
	}
	return 0
}

// CompareVersions orders version strings: semver precedence, a leading "v"
// ignored; a string that is not a version is older than any version.
func CompareVersions(a, b string) int {
	va, vb := parseSemver(a), parseSemver(b)
	switch {
	case va.ok && vb.ok:
		return compareSemver(va, vb)
	case va.ok:
		return 1
	case vb.ok:
		return -1
	}
	return strings.Compare(a, b)
}

// SortReleases sorts releases newest first: by appVersion, then by
// addonVersion.
func SortReleases(rs []AddonRelease) {
	sort.SliceStable(rs, func(i, j int) bool {
		if c := CompareVersions(rs[i].AppVersion, rs[j].AppVersion); c != 0 {
			return c > 0
		}
		return CompareVersions(rs[i].AddonVersion, rs[j].AddonVersion) > 0
	})
}

// LatestRelease is the release to install when no version is given: the
// newest ACTIVE one; without one, the newest EXPERIMENTAL, END_OF_SUPPORT or
// unmarked one. END_OF_LIFE and INACTIVE releases are never picked.
func LatestRelease(rs []AddonRelease) (AddonRelease, bool) {
	sorted := append([]AddonRelease(nil), rs...)
	SortReleases(sorted)
	for _, r := range sorted {
		if NormalizeMode(r.Mode) == ModeActive {
			return r, true
		}
	}
	for _, r := range sorted {
		switch NormalizeMode(r.Mode) {
		case ModeEndOfLife, ModeInactive:
		default:
			return r, true
		}
	}
	return AddonRelease{}, false
}

// FindRelease finds the release with this appVersion, else this
// addonVersion; a leading "v" does not matter. The first match wins: sort
// the releases first (SortReleases), so that it is the newest addonVersion.
func FindRelease(rs []AddonRelease, version string) (AddonRelease, bool) {
	trim := func(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "v") }
	want := trim(version)
	for _, r := range rs {
		if trim(r.AppVersion) == want {
			return r, true
		}
	}
	for _, r := range rs {
		if trim(r.AddonVersion) == want {
			return r, true
		}
	}
	return AddonRelease{}, false
}

// CheckKubeVersion reports whether a cluster's Kubernetes version satisfies
// a release's kubeVersions range (">=1.26.0-0", ">=1.25 <1.31", "~1.28",
// "^1.2", "1.29.x", alternatives with "||"). known is false when either
// side cannot be read; then ok means nothing.
//
// Pre-release parts are ignored on both sides: "-0" in a range is Helm's
// way to admit pre-release clusters, and a distribution suffix of the
// cluster version ("1.30.2-cloudru.1") must not make it older than 1.30.2.
func CheckKubeVersion(constraint, version string) (ok, known bool) {
	v := parseSemver(version)
	if !v.ok || strings.TrimSpace(constraint) == "" {
		return false, false
	}
	v.pre = nil
	for _, group := range strings.Split(constraint, "||") {
		match, known := checkGroup(group, v)
		if !known {
			return false, false
		}
		if match {
			return true, true
		}
	}
	return false, true
}

// checkGroup checks comparators that must all hold ("&&" of a group).
func checkGroup(group string, v semver) (bool, bool) {
	tokens := strings.Fields(strings.ReplaceAll(group, ",", " "))
	if len(tokens) == 0 {
		return false, false
	}
	all := true
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		// ">= 1.26": the operator on its own.
		if strings.Trim(tok, "<>=!~^") == "" && i+1 < len(tokens) {
			i++
			tok += tokens[i]
		}
		match, known := checkComparator(tok, v)
		if !known {
			return false, false
		}
		all = all && match
	}
	return all, true
}

func checkComparator(c string, v semver) (bool, bool) {
	op := ""
	for _, o := range []string{">=", "<=", "!=", "==", ">", "<", "=", "~", "^"} {
		if strings.HasPrefix(c, o) {
			op, c = o, strings.TrimSpace(c[len(o):])
			break
		}
	}
	if c == "*" || c == "x" || c == "X" {
		return true, true
	}
	// "1.29.x": the given parts before the wildcard, as with "~".
	if fields := strings.Split(c, "."); len(fields) > 1 {
		if last := fields[len(fields)-1]; last == "x" || last == "X" || last == "*" {
			if op != "" && op != "=" && op != "==" {
				return false, false
			}
			op, c = "~", strings.Join(fields[:len(fields)-1], ".")
		}
	}
	w := parseSemver(c)
	if !w.ok {
		return false, false
	}
	w.pre = nil
	cmp := cmpNums(v, w)
	// A partial version is a wildcard over the missing parts, as in Helm
	// (Masterminds/semver): "1.29" and "=1.29" are 1.29.x, "<=1.30" admits
	// 1.30.7, ">1.29" starts at 1.30.0. A false refusal is worse than a
	// miss: add-addon would not install at all.
	if w.parts < 3 && op != ">=" && op != "<" && op != "~" && op != "^" {
		next := w
		if w.parts >= 2 {
			next.nums = [3]int{w.nums[0], w.nums[1] + 1, 0}
		} else {
			next.nums = [3]int{w.nums[0] + 1, 0, 0}
		}
		in := cmp >= 0 && cmpNums(v, next) < 0
		switch op {
		case "", "=", "==":
			return in, true
		case "!=":
			return !in, true
		case "<=":
			return cmpNums(v, next) < 0, true
		case ">":
			return cmpNums(v, next) >= 0, true
		}
	}
	switch op {
	case ">=":
		return cmp >= 0, true
	case "<=":
		return cmp <= 0, true
	case ">":
		return cmp > 0, true
	case "<":
		return cmp < 0, true
	case "!=":
		return cmp != 0, true
	case "", "=", "==":
		return cmp == 0, true
	case "~":
		// ~1.2.3, ~1.2: the same minor; ~1: the same major.
		upper := w
		if w.parts >= 2 {
			upper.nums = [3]int{w.nums[0], w.nums[1] + 1, 0}
		} else {
			upper.nums = [3]int{w.nums[0] + 1, 0, 0}
		}
		return cmp >= 0 && cmpNums(v, upper) < 0, true
	case "^":
		// ^1.2.3: the same major; ^0.2.3: the same minor; ^0.0.3: exactly.
		upper := w
		switch {
		case w.nums[0] > 0 || w.parts == 1:
			upper.nums = [3]int{w.nums[0] + 1, 0, 0}
		case w.nums[1] > 0 || w.parts == 2:
			upper.nums = [3]int{0, w.nums[1] + 1, 0}
		default:
			upper.nums = [3]int{0, 0, w.nums[2] + 1}
		}
		return cmp >= 0 && cmpNums(v, upper) < 0, true
	}
	return false, false
}

// ResolveModeValue turns a resolve mode as a person writes it ("overwrite")
// into the value sent to the API: RESOLVE_CONFLICTS_MODE_* (mk8s
// openapi-v3, CreateClusterAddonRequest.resolveMode). "" is the default,
// NONE.
func ResolveModeValue(m string) (string, error) {
	m = strings.ToUpper(strings.TrimSpace(m))
	if m == "" {
		return DefaultResolveMode, nil
	}
	m = strings.TrimPrefix(strings.TrimPrefix(m, "RESOLVE_CONFLICTS_MODE_"), "RESOLVE_MODE_")
	for _, known := range ResolveModes {
		if m == known {
			return "RESOLVE_CONFLICTS_MODE_" + m, nil
		}
	}
	return "", fmt.Errorf(i18n.T("resolve_mode must be one of %s, got %q", "resolve_mode должен быть одним из: %s, передано %q"), strings.Join(ResolveModes, ", "), m)
}
