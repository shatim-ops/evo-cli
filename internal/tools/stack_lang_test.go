// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// The errors of the stack's job store speak the CLI's language (1.3.1);
// their reason does not depend on it: a translated "parse job" read as an
// answer of an API would turn USAGE into API_ERROR (#354).
func TestStackErrors_ReasonDoesNotDependOnLanguage(t *testing.T) {
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
	calls := map[string]func() error{
		"bad id": func() error { _, err := stack.LoadJob("../../etc/passwd"); return err },
		"broken": func() error { _, err := stack.LoadJob(broken); return err },
		"cycle": func() error {
			return (&stack.Job{Steps: []*stack.Step{{Key: "a", DependsOn: []string{"b"}}, {Key: "b", DependsOn: []string{"a"}}}}).CheckDeps()
		},
	}
	reason := func(err error) any {
		var m map[string]any
		_ = json.Unmarshal([]byte(errResult(err)), &m)
		return m["reason"]
	}
	defer i18n.SetLang(i18n.Lang())
	for name, call := range calls {
		i18n.SetLang(i18n.EN)
		en := reason(call())
		i18n.SetLang(i18n.RU)
		if ru := reason(call()); ru != en || en != "USAGE" {
			t.Errorf("%s: reason %v in English, %v in Russian, want USAGE in both", name, en, ru)
		}
	}
}
