// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// A local command that writes to the cloud names itself in creates of the
// inventory by its path, «evo login» (TestWorkflows_WritesDeclareCreates):
// the name is a command of localCommands — a tool would be in the
// registry, and a typo would declare nothing.
func TestLocalCommands_CreatesAreCommands(t *testing.T) {
	var contract struct {
		Services []struct {
			Name     string `json:"name"`
			Requests []struct {
				Method  string   `json:"method"`
				Path    string   `json:"path"`
				Creates []string `json:"creates"`
			} `json:"requests"`
		} `json:"services"`
	}
	if err := json.Unmarshal(mustReadRepo(t, "scripts/api-contract.json"), &contract); err != nil {
		t.Fatal(err)
	}
	local := map[string]bool{}
	for _, lc := range localCommands {
		local[lc.Path] = true
	}
	for _, svc := range contract.Services {
		for _, r := range svc.Requests {
			for _, c := range r.Creates {
				if strings.HasPrefix(c, "evo ") && !local[c] {
					t.Errorf("%s %s %s: creates names %q, which is no command of localCommands", svc.Name, r.Method, r.Path, c)
				}
			}
		}
	}
}
