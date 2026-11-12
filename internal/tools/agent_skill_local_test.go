// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
)

// cardLocal is what a card answered over MCP does not carry (release plan
// 1.9, «Карточки групп — и по MCP»; «Риск», conclusion 160): the answer of
// a server goes to a client that may have no terminal, and names nothing
// of the machine evo runs on or of one cloud — no id, no address, no path
// or file of the configuration, no variable of the environment, no
// profile. A note that needs one of them is a note of the command's card
// (docs/agent-cards), not of the tool's answer. Each: what it is → how it
// is found.
var cardLocal = map[string]*regexp.Regexp{
	"an id (UUID)":                    regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`),
	"an address (IPv4)":               regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
	"a file of evo's configuration":   regexp.MustCompile(`\.evo(\.yaml|/)|\.cloudru-vm|state\.json|credentials`),
	"a path of the machine":           regexp.MustCompile(`(^|[\s` + "`" + `(])(~|/home|/etc|/root|/Users|[A-Z]:\\)[/\\]`),
	"a variable of the environment":   regexp.MustCompile(`\b(CLOUDRU|EVO|EVOLUTION)_[A-Z_]+\b`),
	"the profile of the machine":      regexp.MustCompile(`(?i)\bprofiles?\b`),
	"the name of a field of the key":  regexp.MustCompile(`\b(key_id|key_secret|access_key|secret_key)\b`),
	"a header or a token of a caller": regexp.MustCompile(`(?i)\bbearer\b|authorization:`),
}

// Found by the text of every group's card as agent_skill answers it over
// MCP — the declarations of the registry and the notes of the group
// (cardNotes) alike: a new note, a new group and a new column of the card
// are read the day they are written.
func TestAgentSkill_CardOverMCPCarriesNothingLocal(t *testing.T) {
	r := New(nil)
	tl, ok := r.Lookup("agent_skill")
	if !ok {
		t.Fatal("no tool agent_skill")
	}
	read := 0
	for _, g := range Groups() {
		text, isErr := tl.Handler(context.Background(), map[string]any{"group": g})
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		card, _ := m["skill"].(string)
		if isErr || card == "" {
			t.Errorf("%s: no card: %s", g, text)
			continue
		}
		read++
		for what, re := range cardLocal {
			if found := re.FindString(card); found != "" {
				t.Errorf("%s: the card answered over MCP carries %s (%q): the answer of a server names nothing of the machine evo runs on; keep it in the command's card only, or say it without it", g, what, found)
			}
		}
	}
	if read == 0 {
		t.Fatal("no card was read: the test sees nothing")
	}
}
