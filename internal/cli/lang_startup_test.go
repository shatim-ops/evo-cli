// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// russianCLI runs the test as a person whose language is Russian.
func russianCLI(t *testing.T) {
	t.Helper()
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
}

// The rename notice of the legacy alias is in the language of the
// surface: Russian to a Russian person, English with --json and from
// evo mcp, as everything else of the run (D6; Астра, #2440).
func TestAliasNotice_SpeaksTheLanguageOfTheSurface(t *testing.T) {
	setupCLI(t)
	russianCLI(t)
	legacyAliasRun = true
	t.Cleanup(func() { legacyAliasRun = false })
	for _, c := range []struct {
		args    []string
		english bool
	}{
		{[]string{"version"}, false},
		{[]string{"version", "--json"}, true},
		{[]string{"mcp", "--auth", "bad"}, true},
	} {
		newProcess()
		_, _, errOut := runCLI(t, "", c.args...)
		first, _, _ := strings.Cut(errOut, "\n")
		if !strings.Contains(first, "cloudru-vm") || !strings.Contains(first, "v2.0") {
			t.Fatalf("%v: no rename notice first: %q", c.args, errOut)
		}
		if got := cyrillic.MatchString(first); got == c.english {
			t.Errorf("%v: the notice is not in the language of the surface (english %v): %q", c.args, c.english, first)
		}
	}
}

// legacyConfigDir lays out the settings directory of the legacy name with
// config.yaml made by make, and an absent directory of evo.
func legacyConfigDir(t *testing.T, make func(path string) error) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("AppData", filepath.Join(dir, "AppData"))
	t.Setenv(config.EnvConfigDir, "")
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "CLOUDRU_") {
			t.Setenv(k, "")
		}
	}
	base, err := os.UserConfigDir()
	if err != nil || !strings.HasPrefix(base, dir) {
		t.Skipf("user config dir %q outside the test dir: %v", base, err)
	}
	legacy := filepath.Join(base, config.LegacyAppName)
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := make(filepath.Join(legacy, "config.yaml")); err != nil {
		t.Fatal(err)
	}
}

// The warning of the migration of the legacy settings — copied, or not
// copied — is in the language of the surface: evo mcp chooses English
// before it is printed, and evo's own words in it are translated (Астра,
// #2443, #2444).
func TestMigrationWarning_SpeaksTheLanguageOfTheSurface(t *testing.T) {
	for _, c := range []struct {
		name string
		make func(path string) error
	}{
		{"copied", func(p string) error { return os.WriteFile(p, []byte("profiles: {}\n"), 0o600) }},
		{"not a regular file", func(p string) error { return os.Mkdir(p, 0o700) }},
	} {
		for _, s := range []struct {
			args    []string
			english bool
		}{
			{[]string{"version"}, false},
			{[]string{"version", "--json"}, true},
			{[]string{"mcp", "--toolsets", "bad"}, true},
		} {
			t.Run(c.name+" "+strings.Join(s.args, " "), func(t *testing.T) {
				legacyConfigDir(t, c.make)
				russianCLI(t)
				newProcess()
				_, _, errOut := runCLI(t, "", s.args...)
				line := ""
				for _, l := range strings.Split(errOut, "\n") {
					if strings.Contains(l, config.LegacyAppName) {
						line = l
						break
					}
				}
				if line == "" {
					t.Fatalf("no warning of the migration: %q", errOut)
				}
				// The path is the person's, not a word of evo.
				words := strings.ReplaceAll(line, config.Dir(), "")
				if s.english && cyrillic.MatchString(words) {
					t.Errorf("Russian in the English surface: %q", line)
				}
				if !s.english && strings.Contains(words, "is not a regular file") {
					t.Errorf("English words of evo in the Russian surface: %q", line)
				}
			})
		}
	}
}

// evo mcp healthcheck is a command of the person: its own words around the
// address and the status of the server are in the CLI's language (Астра,
// #2441).
func TestMCPHealthcheck_SpeaksTheLanguageOfTheCLI(t *testing.T) {
	old := httpretry.DefaultBase
	httpretry.DefaultBase = healthWorld{}
	t.Cleanup(func() { httpretry.DefaultBase = old })
	russianCLI(t)
	t.Setenv("MCP_HEALTHCHECK_URL", "")
	code, _, errOut := runCLI(t, "", "mcp", "healthcheck", "--url", "http://127.0.0.1:18080/missing")
	if code == 0 || strings.Contains(errOut, "returned") || !strings.Contains(errOut, "404") {
		t.Errorf("want the Russian refusal with the status: %d %q", code, errOut)
	}
	code, out, errOut := runCLI(t, "", "--json", "mcp", "healthcheck", "--url", "http://127.0.0.1:18080/missing")
	if code == 0 || cyrillic.MatchString(out+errOut) {
		t.Errorf("--json: want English: %d %q %q", code, out, errOut)
	}
}

// The usage line of a command is English in the English help: evo project
// use named its argument «имя» (Астра, #2445).
func TestHelp_EnglishUseHasNoRussian(t *testing.T) {
	russianCLI(t)
	_, out, _ := runCLI(t, "", "project", "use", "--help", "--json")
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "evo project use") && cyrillic.MatchString(line) {
			t.Errorf("Russian in the English usage: %q", line)
		}
	}
}

func TestToolsCall_HelpResultAndErrorLanguage(t *testing.T) {
	setupCLI(t)
	for _, lang := range []string{i18n.RU, i18n.EN} {
		old := i18n.Lang()
		t.Cleanup(func() { i18n.SetLang(old) })
		i18n.SetLang(lang)
		for _, path := range [][]string{{"tools", "--help"}, {"tools", "call", "--help"}} {
			code, out, errOut := runCLI(t, "", path...)
			if code != 0 || !strings.Contains(out, "EVO_LANG=en") || !strings.Contains(out, "--json") ||
				!strings.Contains(out, map[string]string{i18n.RU: "диагност", i18n.EN: "diagnostic"}[lang]) {
				t.Errorf("%s %v: language of tool answers and diagnostics missing: %s%s", lang, path, out, errOut)
			}
		}
		for _, machine := range []bool{false, true} {
			args := []string{"tools", "call", "history"}
			if machine {
				args = append(args, "--json")
			}
			code, out, errOut := runCLI(t, "", args...)
			var history struct{ Note string }
			if code != 0 || json.Unmarshal([]byte(out), &history) != nil || history.Note == "" ||
				cyrillic.MatchString(history.Note) != (lang == i18n.RU && !machine) {
				t.Errorf("%s %v: history note in result: %q, stderr %q", lang, args, history.Note, errOut)
			}
			args = []string{"tools", "call", "vm_get", "--arg", "vm_id=" + parityVM, "--arg", "name=web"}
			if machine {
				args = append(args, "--json")
			}
			code, out, errOut = runCLI(t, "", args...)
			if code != clerrors.ExitGeneral || errOut == "" || (machine && out != "") {
				t.Fatalf("%s %v exit %d out=%q stderr=%q", lang, args, code, out, errOut)
			}
			if !machine {
				var answer struct{ Error, Hint, Reason string }
				if json.Unmarshal([]byte(out), &answer) != nil || answer.Reason != clerrors.CodeUsage || answer.Hint == "" || answer.Error == "" ||
					cyrillic.MatchString(answer.Error) != (lang == i18n.RU) || cyrillic.MatchString(answer.Hint) != (lang == i18n.RU) {
					t.Errorf("%s %v: tool error body %q", lang, args, out)
				}
			}
			if cyrillic.MatchString(errOut) != (lang == i18n.RU && !machine) {
				t.Errorf("%s %v: diagnostic %q", lang, args, errOut)
			}
		}
	}
}
