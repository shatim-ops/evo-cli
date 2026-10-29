// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

const (
	cardsDir  = repoRoot + "/docs/agent-cards"
	cardsHint = "go test ./internal/cli -run TestGroupCardsDoc -update-docs"
)

// #515, option A, layer B: docs/agent-cards/<group>.md is the card 'evo agent
// skill --group <group>' prints, for every group and nothing else, as
// docs/agent-skill.md is the rules (the copies a person reads in the
// repository; the binary is the source).
func TestGroupCardsDoc(t *testing.T) {
	want := map[string]string{}
	for _, g := range tools.Groups() {
		card, _ := tools.GroupCard(g)
		want[g+".md"] = card
	}
	if *updateDocs {
		_ = os.RemoveAll(cardsDir)
		if err := os.MkdirAll(cardsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, card := range want {
			if err := os.WriteFile(filepath.Join(cardsDir, name), []byte(card), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	entries, err := os.ReadDir(cardsDir)
	if err != nil {
		t.Fatalf("%v; generate them: %s", err, cardsHint)
	}
	for _, e := range entries {
		if _, ok := want[e.Name()]; !ok {
			t.Errorf("docs/agent-cards/%s: no group of that name; regenerate: %s", e.Name(), cardsHint)
		}
	}
	for name, card := range want {
		got, err := os.ReadFile(filepath.Join(cardsDir, name))
		got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
		if err != nil || string(got) != card {
			t.Errorf("docs/agent-cards/%s differs from 'evo agent skill --group'; regenerate: %s", name, cardsHint)
		}
	}
}

// 'evo agent skill --group <g>' prints the card of a group; --json wraps it
// as the rules; a group that is not there is USAGE with the groups.
func TestAgentSkill_GroupPrintsTheCard(t *testing.T) {
	card, _ := tools.GroupCard("pg")
	if code, stdout, stderr := runCLI(t, "", "agent", "skill", "--group", "pg"); code != 0 || stdout != card {
		t.Errorf("exit %d: %s%s", code, stdout, stderr)
	}
	var doc struct{ Skill string }
	code, stdout, _ := runCLI(t, "", "agent", "skill", "--group", "pg", "--json")
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 || doc.Skill != card {
		t.Errorf("--json: exit %d, %v", code, err)
	}
	code, stdout, stderr := runCLI(t, "", "agent", "skill", "--group", "nope")
	if code != clerrors.ExitGeneral || !strings.Contains(stdout+stderr, "pg") {
		t.Errorf("unknown group: exit %d: %s%s", code, stdout, stderr)
	}
}

// 'evo agent setup --skill' for a client whose rules are a skill of their
// own writes the cards of the groups the agent sees next to SKILL.md, and
// the rules say where they are (progressive disclosure: the rules always,
// a card when the task needs its group).
func TestAgentSetup_SkillWritesTheCardsOfTheToolsets(t *testing.T) {
	setupAgent(t)
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--skill", "--toolsets", "pg,vpc", "--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	dir := filepath.Join(".claude", "skills", "evo", "cards")
	for _, g := range []string{"pg", "vpc"} {
		card, _ := tools.GroupCard(g)
		if got := string(fileBytes(t, filepath.Join(dir, g+".md"))); got != card {
			t.Errorf("cards/%s.md is not the card:\n%s", g, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "k8s.md")); err == nil {
		t.Error("cards/k8s.md written for a group the agent does not see")
	}
	if skill := string(fileBytes(t, filepath.Join(".claude", "skills", "evo", "SKILL.md"))); !strings.Contains(skill, "evo agent skill --group") {
		t.Error("the rules do not say where the cards are")
	}
}
