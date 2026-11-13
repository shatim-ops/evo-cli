// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// agentEntryChecks checks evo's entry in the file of each client that
// starts evo by its full path (agentClient.fullPath: Claude Desktop): the
// app does not read PATH, and when the path is gone — an update, a move —
// it stops starting evo without a word of evo's («Риск», conclusion 31,
// recommendation (б)). One check per entry: the evo it starts is there and
// is this one, and the profile of its --profile is there. A client with no
// file on this system, or no entry of evo in it, is not checked: evo was
// not connected there. A warning, never a failure of the key.
func agentEntryChecks() []tools.DoctorCheck {
	var checks []tools.DoctorCheck
	for _, c := range agentClients {
		if !c.fullPath {
			continue
		}
		files := c.files()
		for _, s := range c.scopes() {
			path, err := files[s].path()
			if err != nil {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if ch, ok := c.entryCheck(path, data); ok {
				checks = append(checks, ch)
			}
		}
	}
	return checks
}

// entryCheck is the check of evo's entry in data, the client's file at
// path; false when the file has no entry of evo.
func (c agentClient) entryCheck(path string, data []byte) (tools.DoctorCheck, bool) {
	warn := func(detail string) (tools.DoctorCheck, bool) {
		return tools.DoctorCheck{Name: c.title, Status: tools.CheckWarn, Detail: detail}, true
	}
	again := "evo agent setup --client " + c.name
	var doc map[string]json.RawMessage
	var servers map[string]json.RawMessage
	if json.Unmarshal(data, &doc) != nil || (doc[c.servers] != nil && json.Unmarshal(doc[c.servers], &servers) != nil) {
		return warn(i18n.Tf("%s is not JSON the app can read either; fix it, or run: %s", "%s — не JSON, приложение его тоже не прочитает; исправьте файл или выполните: %s", path, again))
	}
	raw, ok := servers[mcpServerName]
	if !ok {
		return tools.DoctorCheck{}, false
	}
	var e struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Command == "" {
		return warn(i18n.Tf("the entry %q in %s starts no evo; run: %s", "запись %q в %s не запускает evo; выполните: %s", mcpServerName, path, again))
	}
	if st, err := os.Stat(e.Command); err != nil || st.IsDir() {
		return warn(i18n.Tf("the app starts %s, which is not there (moved, or replaced by an update): the app cannot start evo; run: %s",
			"приложение запускает %s, а его нет (перенесён или заменён обновлением): приложение не запустит evo; выполните: %s", e.Command, again))
	}
	if self, err := executable(); err == nil && sameFile(self) != sameFile(e.Command) {
		return warn(i18n.Tf("the app starts %s (%s), not this evo (%s); to start this one, run: %s",
			"приложение запускает %s (%s), а не этот evo (%s); чтобы запускало этот, выполните: %s", e.Command, evoVersionOf(e.Command), self, again))
	}
	profile := ""
	for i, a := range e.Args {
		if a == "--profile" && i+1 < len(e.Args) {
			profile = e.Args[i+1]
		}
	}
	if profile == "" {
		return tools.DoctorCheck{Name: c.title, Status: tools.CheckOK, Detail: e.Command}, true
	}
	// The app does not pass the shell's variables: its evo reads the
	// profiles of the default directory, whatever CLOUDRU_CONFIG_DIR says
	// here (docs/mcp.md).
	if dir := config.DefaultDir(); dir != "" {
		if found, err := config.HasProfile(dir, profile); err == nil && !found {
			return warn(i18n.Tf("the app starts evo with the profile %q, which is not in %s (the app does not get CLOUDRU_CONFIG_DIR): create it there (evo configure --profile %s), or run %s with a profile that is there",
				"приложение запускает evo с профилем %q, а его нет в %s (CLOUDRU_CONFIG_DIR приложение не получает): создайте его там (evo configure --profile %s) или выполните %s с существующим профилем", profile, dir, profile, again))
		}
	}
	return tools.DoctorCheck{Name: c.title, Status: tools.CheckOK, Detail: i18n.Tf("%s, profile %s", "%s, профиль %s", e.Command, profile)}, true
}
