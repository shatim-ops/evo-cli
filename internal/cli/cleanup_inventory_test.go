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
)

// cleanupNotListed are the lists of created things skills/evo-cleanup does
// not read, with why.
var cleanupNotListed = map[string]string{
	"pg_backups":    "evo has no tool that deletes a backup; whether a backup outlives pg_delete is not checked live (a probe for 1.6)",
	"k8s_nodepools": "evo has no tool that deletes a node pool: the pools go with their cluster, which the inventory names (k8s_list)",
}

// Release plan 1.5, С9: the inventory of skills/evo-cleanup reads the list
// of everything a tool of evo creates (the list of each create in
// docs/internal/contract-map.json, axis 3): a new create whose list the
// skill does not name leaves its resources out of «remove everything».
func TestCleanup_InventoryReadsEveryCreatedList(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "internal", "contract-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tools map[string]struct {
		Creates *struct {
			List string `json:"list"`
		} `json:"creates"`
	}
	if err := json.Unmarshal(data, &tools); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, f := range []string{"SKILL.md", filepath.Join("references", "delete-map.md")} {
		b, err := os.ReadFile(filepath.Join("..", "..", "skills", "evo-cleanup", f))
		if err != nil {
			t.Fatal(err)
		}
		text.Write(b)
	}
	seen := map[string]bool{}
	for name, tl := range tools {
		if tl.Creates == nil || tl.Creates.List == "" {
			continue
		}
		list := tl.Creates.List
		seen[list] = true
		if _, ok := cleanupNotListed[list]; ok {
			continue
		}
		if !strings.Contains(text.String(), "`"+list+"`") {
			t.Errorf("%s creates what %s lists, and skills/evo-cleanup does not read %s: add it to the inventory, or to cleanupNotListed with why", name, list, list)
		}
	}
	for list := range cleanupNotListed {
		if !seen[list] {
			t.Errorf("cleanupNotListed has %s, which no create lists: remove the line", list)
		}
	}
}
