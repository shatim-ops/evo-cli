// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.14.0", "1.13.9", 1},
		{"1.2.10", "1.2.9", 1},
		{"1.2", "1.2.0", 0},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-alpha", "1.0.0-1", 1},
		{"1.0.0+build.5", "1.0.0", 0},
		{"latest", "0.0.1", -1},
		{"b", "a", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// Порядок релизов в ответе не задан: новые — первыми по appVersion, затем
// по addonVersion; «последняя» — новейшая ACTIVE.
func TestSortAndLatestRelease(t *testing.T) {
	rs := []AddonRelease{
		{AppVersion: "v1.9.0", AddonVersion: "1", Mode: "SUPPORT_MODE_ACTIVE"},
		{AppVersion: "v1.10.0", AddonVersion: "1", Mode: "SUPPORT_MODE_EXPERIMENTAL"},
		{AppVersion: "v1.9.0", AddonVersion: "2", Mode: "ACTIVE"},
		{AppVersion: "v1.8.0", AddonVersion: "1", Mode: "SUPPORT_MODE_END_OF_LIFE"},
	}
	sorted := append([]AddonRelease(nil), rs...)
	SortReleases(sorted)
	order := ""
	for _, r := range sorted {
		order += r.AppVersion + "/" + r.AddonVersion + " "
	}
	if order != "v1.10.0/1 v1.9.0/2 v1.9.0/1 v1.8.0/1 " {
		t.Errorf("order: %s", order)
	}
	if l, ok := LatestRelease(rs); !ok || l.AppVersion != "v1.9.0" || l.AddonVersion != "2" {
		t.Errorf("latest: %+v %v", l, ok)
	}
	if l, ok := LatestRelease(rs[1:2]); !ok || l.AppVersion != "v1.10.0" {
		t.Errorf("latest without ACTIVE: %+v %v", l, ok)
	}
	if _, ok := LatestRelease(rs[3:]); ok {
		t.Error("an END_OF_LIFE release picked as latest")
	}
	if r, ok := FindRelease(rs, "1.10.0"); !ok || r.AppVersion != "v1.10.0" {
		t.Errorf("find by appVersion without v: %+v %v", r, ok)
	}
	if r, ok := FindRelease(rs, "2"); !ok || r.AddonVersion != "2" {
		t.Errorf("find by addonVersion: %+v %v", r, ok)
	}
	if _, ok := FindRelease(rs, "3.0"); ok {
		t.Error("found a version that is not there")
	}
}

func TestNormalizeMode(t *testing.T) {
	for in, want := range map[string]string{"SUPPORT_MODE_ACTIVE": "ACTIVE", "active": "ACTIVE", " SUPPORT_MODE_END_OF_LIFE ": "END_OF_LIFE", "": ""} {
		if got := NormalizeMode(in); got != want {
			t.Errorf("NormalizeMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckKubeVersion(t *testing.T) {
	cases := []struct {
		constraint, version string
		ok, known           bool
	}{
		{">=1.26.0-0", "v1.34.6", true, true},
		{">=1.26.0-0", "v1.25.9", false, true},
		{">=1.26.0-0", "1.26.0-cloudru.1", true, true},
		{">= 1.25, < 1.31", "v1.30.2", true, true},
		{">=1.25 <1.31", "v1.31.0", false, true},
		{"<1.31.0", "1.31.0-rc.1", false, true},
		{"~1.28", "1.28.9", true, true},
		{"~1.28.2", "1.29.0", false, true},
		{"^1.2", "1.40.0", true, true},
		{"^0.2.3", "0.3.0", false, true},
		{"1.29.x", "1.29.4", true, true},
		{"1.29.x", "1.30.0", false, true},
		{"<1.20 || >=1.30", "1.31.1", true, true},
		{"<1.20 || >=1.30", "1.25.0", false, true},
		{"*", "1.0.0", true, true},
		{"=1.30.1", "v1.30.1", true, true},
		{"!=1.30.1", "v1.30.1", false, true},
		// Не читается — неизвестно, а не «не подходит».
		{">=1.26", "", false, false},
		{">=1.26", "latest", false, false},
		{"", "1.30.0", false, false},
		{"> foo", "1.30.0", false, false},
		{"<1.20 || banana", "1.31.1", false, false},
	}
	for _, c := range cases {
		ok, known := CheckKubeVersion(c.constraint, c.version)
		if ok != c.ok || known != c.known {
			t.Errorf("CheckKubeVersion(%q, %q) = %v, %v; want %v, %v", c.constraint, c.version, ok, known, c.ok, c.known)
		}
	}
}

func TestResolveModeValue(t *testing.T) {
	for in, want := range map[string]string{
		"overwrite": "RESOLVE_CONFLICTS_MODE_OVERWRITE", "": "RESOLVE_CONFLICTS_MODE_NONE",
		"RESOLVE_CONFLICTS_MODE_NONE": "RESOLVE_CONFLICTS_MODE_NONE", "none": "RESOLVE_CONFLICTS_MODE_NONE",
	} {
		if got, err := ResolveModeValue(in); err != nil || got != want {
			t.Errorf("ResolveModeValue(%q) = %q, %v", in, got, err)
		}
	}
	// PRESERVE к добавлению плагина неприменим (спецификация).
	for _, bad := range []string{"merge", "preserve"} {
		if _, err := ResolveModeValue(bad); err == nil {
			t.Errorf("resolve mode %q accepted", bad)
		}
	}
}

// Два релиза одной appVersion: после SortReleases FindRelease берёт новый
// addonVersion, а не первый в порядке API (ревью Qwen на #78).
func TestFindRelease_SameAppVersionTakesNewestAddonVersion(t *testing.T) {
	rs := []AddonRelease{
		{AppVersion: "1.14.0", AddonVersion: "1.14.0-1"},
		{AppVersion: "1.14.0", AddonVersion: "1.14.0-3"},
		{AppVersion: "1.13.0", AddonVersion: "1.13.0-2"},
	}
	SortReleases(rs)
	r, ok := FindRelease(rs, "v1.14.0")
	if !ok || r.AddonVersion != "1.14.0-3" {
		t.Fatalf("FindRelease = %+v, %v; want addon 1.14.0-3", r, ok)
	}
}

// Частичная версия — шаблон по недостающим частям, как в Helm
// (Masterminds/semver): ложный отказ хуже пропуска (ревью Grok на #78).
func TestCheckKubeVersion_PartialVersionsAsHelm(t *testing.T) {
	for _, c := range []struct {
		constraint, version string
		ok                  bool
	}{
		{"<=1.30", "1.30.7", true},
		{"<=1.30", "1.31.0", false},
		{"1.29", "1.29.4", true},
		{"=1.29", "1.29.4", true},
		{"=1.29", "1.30.0", false},
		{">1.29", "1.29.5", false},
		{">1.29", "1.30.0", true},
		{"!=1.29", "1.29.5", false},
		{">=1.29", "1.29.0", true},
		{"<1.30", "1.30.1", false},
		{"<=1", "1.34.2", true},
		{">1", "1.99.0", false},
		{"<=1.30.0", "1.30.7", false},
	} {
		if ok, known := CheckKubeVersion(c.constraint, c.version); !known || ok != c.ok {
			t.Errorf("CheckKubeVersion(%q, %q) = %v, %v; want %v", c.constraint, c.version, ok, known, c.ok)
		}
	}
}
