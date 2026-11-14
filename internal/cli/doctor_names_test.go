// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// checkNamesKept are the names of checks D6 keeps as they are in either
// language, with why.
var checkNamesKept = map[string]string{
	"project_id": "a key of the configuration",
}

// doctorNameRE is a check of doctor named by a literal in the code.
var doctorNameRE = regexp.MustCompile(`DoctorCheck\{Name: "([^"]+)"`)

// The human column of doctor speaks the CLI's language (D6; Astra's list
// #1835): every check named by a literal has a label in checkLabels, or is
// a name D6 keeps as it is (a key, a file, a program, a service). The JSON
// keeps the machine name.
func TestDoctor_CheckNamesSpeakCLILanguage(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang("ru")
	found := 0
	for _, dir := range []string{".", "../tools"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if filepath.Ext(f) != ".go" || regexp.MustCompile(`_test\.go$`).MatchString(f) {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range doctorNameRE.FindAllStringSubmatch(string(src), -1) {
				found++
				name := m[1]
				if _, keep := checkNamesKept[name]; keep {
					continue
				}
				if checkLabel(name) == name {
					t.Errorf("%s: the doctor check %q has no Russian label in checkLabels, nor a reason in checkNamesKept", f, name)
				}
			}
		}
	}
	if found < 10 {
		t.Fatalf("found %d named checks: the scan lost its pattern", found)
	}
	i18n.SetLang("en")
	if got := checkLabel("key term"); got != "key term" {
		t.Errorf("English output: %q, want the name as it is", got)
	}
}
