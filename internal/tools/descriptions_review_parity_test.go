// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReviewParity_RP_5_UnrecordedEnglishDescriptions: MCP tools/list
// English descriptions may differ from v1.1.0 only where CHANGELOG records
// a description change. deploy, status and org_projects grew sentences that
// v1.1.0 did not have: a section newer than 1.1.0 (unreleased or a later
// version) must have a bullet about the descriptions that names each of them.
func TestReviewParity_RP_5_UnrecordedEnglishDescriptions(t *testing.T) {
	b, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob("../../changelog.d/*.md")
	if err != nil {
		t.Fatal(err)
	}
	var fragments []string
	for _, p := range paths {
		if filepath.Base(p) == "README.md" {
			continue
		}
		f, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		fragments = append(fragments, string(f))
	}
	bullets, ok := descriptionBullets(string(b), fragments)
	if !ok {
		t.Fatal("CHANGELOG has no 1.1.0 section")
	}
	recorded := func(name string) bool {
		for _, item := range bullets {
			if strings.Contains(item, "`"+name+"`") {
				return true
			}
		}
		return false
	}
	reg := New(nil)
	for _, c := range []struct{ name, phrase string }{
		{"deploy", "for a new VM and for an update of the existing one alike"},
		{"status", "When the cloud answered, vm_name, zone_name and flavor_name"},
		{"org_projects", "and the active project (active)"},
	} {
		tl, ok := reg.Lookup(c.name)
		if !ok {
			t.Fatalf("no tool %s", c.name)
		}
		if strings.Contains(tl.Description, c.phrase) && !recorded(c.name) {
			t.Errorf("%s description differs from v1.1.0 (contains %q); no CHANGELOG bullet «Описания инструментов MCP …» after 1.1.0 names `%s`",
				c.name, c.phrase, c.name)
		}
	}
}

// descriptionBullets are the «Описания инструментов MCP …» items newer than
// 1.1.0: in CHANGELOG.md above its 1.1.0 section and in the fragments of
// changelog.d, where a PR writes its entry until the release folds it in.
func descriptionBullets(changelog string, fragments []string) ([]string, bool) {
	since, _, ok := strings.Cut(changelog, "\n## [1.1.0]")
	if !ok {
		return nil, false
	}
	var bullets []string
	for _, text := range append([]string{since}, fragments...) {
		for _, item := range strings.Split("\n"+text, "\n- ") {
			if strings.HasPrefix(item, "Описания инструментов MCP") {
				bullets = append(bullets, item)
			}
		}
	}
	return bullets, true
}

func TestReviewParity_RP_5_FragmentBulletsCount(t *testing.T) {
	changelog := "## [Не выпущено]\n\n## [1.1.0] — 2026-10-20\n\n- Описания инструментов MCP: `old`.\n"
	got, ok := descriptionBullets(changelog, []string{"### Изменено\n\n- Описания инструментов MCP: `deploy`.\n"})
	if !ok || len(got) != 1 || !strings.Contains(got[0], "`deploy`") {
		t.Errorf("bullets %q, ok %v; want the fragment's bullet only", got, ok)
	}
}
