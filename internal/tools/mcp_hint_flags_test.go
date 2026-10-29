// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// cliFlag is a long flag of the CLI in a text: "--project-id", "or pass
// --image".
var cliFlag = regexp.MustCompile(`(?:^|[\s'"(])(--[a-z][a-z0-9-]+)`)

// toolHintFlagMisses are the flags of the CLI known to reach the answers of
// tools: flag → the work item of the release plan that fixes it. A flag
// comes from one text shared by many tools (the hint of a missing project,
// of a missing boot image), so the list names the flag, not the tool. The
// list only shrinks: a flag not here fails the test, and so does a line
// whose flag no answer carries.
var toolHintFlagMisses = map[string]string{
	"--image":      "release plan 1.6, «Три формы одним PR» (Ц-31 агентного аудита rc.1): «разработка»",
	"--project-id": "release plan 1.6, «Три формы одним PR» (Ц-31 агентного аудита rc.1): «разработка»",
	"--verbose":    "release plan 1.6, «Три формы одним PR» (Ц-31 агентного аудита rc.1): «разработка»",
	"--all":        "release plan 1.6, «Три формы одним PR» (Ц-31 агентного аудита rc.1): «разработка»",
}

// Ц-31 of the agent's audit of rc.1: deploy over MCP without an image
// answered «… or pass --image»; the agent has no flags, it has the
// argument the answer names in missing (D48). The text of a tool's answer
// is the MCP server's too, so it speaks of arguments, and the CLI adds its
// own words (toolHints): no answer of any tool — error, hint, message,
// warning — names a flag of the CLI, in each world of TestQA11 and in one
// where deploy has a compose file and no image.
func TestToolAnswers_NameNoCLIFlag(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	compose := filepath.Join(t.TempDir(), "docker-compose.yml")
	if err := os.WriteFile(compose, []byte("services:\n  web:\n    image: nginx:1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worlds := []struct {
		kind string
		edit func(*config.Config)
	}{
		{"500", nil}, {"404", nil}, {"200", nil}, {"403", nil},
		// No S3 keys, no image, no project: the CONFIG_INVALID answers.
		{"500", func(c *config.Config) { c.S3 = config.S3Config{}; c.ImageID = ""; c.ProjectID = "" }},
		// deploy reaches the boot image only past its compose file.
		{"200", func(c *config.Config) { c.ImageID = ""; c.Image = ""; c.ComposeFile = compose }},
	}
	var mu sync.Mutex
	found := map[string]string{}
	for _, lang := range []string{i18n.EN, i18n.RU} {
		for _, w := range worlds {
			i18n.SetLang(lang)
			walkAnswers(t, w.kind, w.edit, func(c langCall, m map[string]any) {
				for _, f := range []string{"error", "hint", "message", "warning"} {
					s, _ := m[f].(string)
					for _, sub := range cliFlag.FindAllStringSubmatch(s, -1) {
						mu.Lock()
						if _, ok := found[sub[1]]; !ok {
							b, _ := json.Marshal(c.args)
							found[sub[1]] = fmt.Sprintf("%s %s: %s (args=%.120s)", c.tool, f, clip(s), b)
						}
						mu.Unlock()
					}
				}
			})
		}
	}
	flags := make([]string, 0, len(found))
	for f := range found {
		flags = append(flags, f)
	}
	sort.Strings(flags)
	for _, f := range flags {
		if toolHintFlagMisses[f] == "" {
			t.Errorf("%s in the answer of a tool: %s — name the argument (missing, D48), or, while the plan moves it, add the flag to toolHintFlagMisses with the work item", f, found[f])
		}
	}
	for f := range toolHintFlagMisses {
		if found[f] == "" {
			t.Errorf("toolHintFlagMisses names %s, which no answer of a tool carries: remove the line", f)
		}
	}
}
