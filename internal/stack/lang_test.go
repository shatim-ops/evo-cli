// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package stack

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

var cyrillic = regexp.MustCompile(`\p{Cyrillic}`)

// What the stack says in an error is in the CLI's language; the English
// text stays as it was (D6, 1.3.1: a script may match it, and the MCP
// server answers it).
func TestStackErrorsSpeakCLILanguage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(config.DataDir(home), "stack-jobs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const broken = "stk-0123456789"
	if err := os.WriteFile(filepath.Join(dir, broken+".json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func() error{
		"lock, bad id": func() error { _, err := LockJob("bad", 0); return err },
		"load, bad id": func() error { _, err := LoadJob("bad"); return err },
		"load, broken": func() error { _, err := LoadJob(broken); return err },
		"missing dependency": func() error {
			return (&Job{Steps: []*Step{{Key: "dns", DependsOn: []string{"nlb"}}}}).CheckDeps()
		},
		"cycle": func() error {
			return (&Job{Steps: []*Step{{Key: "a", DependsOn: []string{"b"}}, {Key: "b", DependsOn: []string{"a"}}}}).CheckDeps()
		},
	}
	want := map[string]string{
		"lock, bad id":       `invalid job_id "bad" (want stk-<10 hex digits>, as stack_plan returns)`,
		"load, bad id":       `invalid job_id "bad" (want stk-<10 hex digits>, as stack_plan returns)`,
		"missing dependency": `step "dns" depends on "nlb", which this stack does not have (nlb.target must name a step such as "vm"; dns without an explicit target needs nlb)`,
		"cycle":              "dependency cycle: a → b → a",
	}
	defer i18n.SetLang(i18n.Lang())
	for name, call := range cases {
		i18n.SetLang(i18n.RU)
		if err := call(); err == nil || !cyrillic.MatchString(err.Error()) {
			t.Errorf("%s, Russian CLI: %v", name, err)
		}
		i18n.SetLang(i18n.EN)
		err := call()
		if err == nil || cyrillic.MatchString(err.Error()) || (want[name] != "" && err.Error() != want[name]) {
			t.Errorf("%s, English: %v, want %q", name, err, want[name])
		}
	}
	i18n.SetLang(i18n.RU)
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if _, err := jobsDir(); err == nil || !cyrillic.MatchString(err.Error()) {
		t.Errorf("no home, Russian CLI: %v", err)
	}
}
