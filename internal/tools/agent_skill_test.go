// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// Release plan 1.9, «Карточки групп — и по MCP» («Риск», conclusion 160):
// agent_skill answers the card of a tool group to a client that has no
// terminal. It builds no engine and asks the cloud nothing; every group of
// the registry has a card; the card it answers over MCP is the registry's,
// as the command's is, less what is of a terminal — the column of the
// commands and the pointers to `evo …` — so that the answer of a server
// does not hand the model the way around that server's limits.
func TestAgentSkill_CardOfEveryGroupWithNoTerminalInIt(t *testing.T) {
	engines := 0
	r := New(func(map[string]any) (*engine.Engine, error) {
		engines++
		return nil, errors.New("no engine: agent_skill reads the registry")
	})
	r.SetStateDir(t.TempDir())
	tl, ok := r.Lookup("agent_skill")
	if !ok {
		t.Fatal("no tool agent_skill")
	}
	if tl.Mutating || HasSideEffects(tl) || IsDestructive(tl) || takesConsent(tl) || tl.Group != "auth" {
		t.Errorf("agent_skill is a read of the group auth with no consent: group %q", tl.Group)
	}
	call := func(ctx context.Context, args map[string]any) (map[string]any, bool, string) {
		t.Helper()
		text, isErr := tl.Handler(ctx, args)
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		return m, isErr, text
	}
	terminal := regexp.MustCompile("`evo [a-z]|evo tools describe|evo agent skill|\\| Command \\|")
	known := map[string]bool{}
	for _, x := range r.Tools() {
		known[x.Name] = true
	}
	named := regexp.MustCompile("(?m)^\\| `([a-z0-9_]+)`")
	for _, g := range Groups() {
		m, isErr, text := call(context.Background(), map[string]any{"group": g})
		card, _ := m["skill"].(string)
		if isErr || m["group"] != g || card == "" {
			t.Errorf("%s: no card: %s", g, text)
			continue
		}
		if loc := terminal.FindString(card); loc != "" {
			t.Errorf("%s: the card answered over MCP names the terminal (%q): the answer of a server is not the way around it", g, loc)
		}
		rows := named.FindAllStringSubmatch(card, -1)
		if len(rows) == 0 {
			t.Errorf("%s: the card names no tool", g)
		}
		for _, row := range rows {
			if !known[row[1]] {
				t.Errorf("%s: the card names %s, which is no tool", g, row[1])
			}
		}
		if len(card) > cardLimit {
			t.Errorf("%s: a card of %d bytes, over the limit of %d: the notes are a line each, not a manual", g, len(card), cardLimit)
		}
		if !strings.Contains(card, "not a preview") {
			t.Errorf("%s: the card does not say it is no preview and no ground for confirmed=true", g)
		}
		// The command's card is the one of docs/agent-cards, with the
		// commands: the same rows, another surface.
		cli, _, _ := call(WithCaller(context.Background(), &Caller{}), map[string]any{"group": g})
		if want, _ := GroupCard(g); cli["skill"] != want {
			t.Errorf("%s: the card of the command is not GroupCard", g)
		}
	}
	// No group: the groups, each with how many tools it has.
	m, isErr, text := call(context.Background(), map[string]any{})
	groups, _ := m["groups"].([]any)
	if isErr || len(groups) != len(Groups()) {
		t.Fatalf("no group: want the %d groups: %s", len(Groups()), text)
	}
	// A group that is not: USAGE naming the groups.
	if m, isErr, text := call(context.Background(), map[string]any{"group": "nope"}); !isErr || m["reason"] != "USAGE" || !strings.Contains(text, "vm") {
		t.Errorf("an unknown group: want USAGE naming the groups: %s", text)
	}
	if engines != 0 {
		t.Errorf("agent_skill built an engine %d times: it reads the registry only", engines)
	}
}

// A tool the server does not serve (--toolsets, --read-only) is marked by
// its name in the card, not by one line for the group: a model that reads
// a tool in a card and does not find it in tools/list looks for another
// way («Риск», conclusion 160, У2).
func TestAgentSkill_MarksEveryToolTheServerDoesNotServe(t *testing.T) {
	r := New(nil)
	r.SetServed(func(name string) bool { return name == "vm_list" || name == "agent_skill" })
	tl, _ := r.Lookup("agent_skill")
	text, _ := tl.Handler(context.Background(), map[string]any{"group": "vm"})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	card, _ := m["skill"].(string)
	for _, line := range strings.Split(card, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		served := strings.HasPrefix(line, "| `vm_list`")
		if marked := strings.Contains(line, "not on this server"); marked == served {
			t.Errorf("served=%v marked=%v: %s", served, marked, line)
		}
	}
	// The other columns name tools of other groups (deleted with destroy,
	// ids from image_list) with no mark: the card says so once.
	if !strings.Contains(card, "the person's choice") || !strings.Contains(card, "Tools named in the other columns may be absent too: tools/list is what this server has.") {
		t.Errorf("the card does not say whose choice the limits of the server are, or that the other columns name tools unmarked: %s", card)
	}
	// A group the server serves no tool of: every row marked, the same
	// lines above the table.
	text, _ = tl.Handler(context.Background(), map[string]any{"group": "pg"})
	_ = json.Unmarshal([]byte(text), &m)
	other, _ := m["skill"].(string)
	if strings.Count(other, "(not on this server)") != strings.Count(other, "\n| `") || !strings.Contains(other, "the person's choice") {
		t.Errorf("a group with no tool on the server: not every row is marked, or the card does not say whose choice it is: %s", other)
	}
	// The groups say how many of their tools the server serves.
	text, _ = tl.Handler(context.Background(), map[string]any{})
	if !strings.Contains(text, `"on_server"`) {
		t.Errorf("the groups do not say how many tools the server serves: %s", text)
	}
}

// A server bound to its projects (--allow-project) answers a repeat of a
// delete NOT_FOUND, not already_deleted (H9, alreadyDeleted): the card it
// gives says what its own deletes answer (FB19I-26 (а)). A server not
// bound says already_deleted, as before.
func TestAgentSkill_TheCardSaysWhatARepeatOfADeleteAnswersOnThisServer(t *testing.T) {
	for _, bound := range []bool{false, true} {
		r := New(nil)
		if bound {
			r.SetProjectBound()
		}
		tl, _ := r.Lookup("agent_skill")
		text, _ := tl.Handler(context.Background(), map[string]any{"group": "pg"})
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		card, _ := m["skill"].(string)
		says := strings.Contains(card, "a repeat answers `already_deleted`")
		saysNotFound := strings.Contains(card, "a repeat answers `NOT_FOUND`")
		if says == bound || saysNotFound != bound {
			t.Errorf("bound=%v: the card says already_deleted %v, NOT_FOUND %v: %s", bound, says, saysNotFound, card)
		}
	}
}

// A server of one group (--toolsets k8s) serves every tool of it, and the
// card still names tools of other groups in its other columns and notes
// (snat_create, destroy, vm_list): each name of a registry tool in the
// card is served, marked, or under the line that says the other columns
// may name tools the server lacks (FB19I-25; «Риск», conclusion 160, У2).
func TestAgentSkill_EveryNameInTheCardIsServedOrCovered(t *testing.T) {
	name := regexp.MustCompile("`([a-z0-9_]+)`")
	for _, g := range Groups() {
		r := New(nil)
		served := func(n string) bool {
			tl, ok := r.Lookup(n)
			return n == "agent_skill" || ok && tl.Group == g
		}
		r.SetServed(served)
		tl, _ := r.Lookup("agent_skill")
		text, _ := tl.Handler(context.Background(), map[string]any{"group": g})
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		card, _ := m["skill"].(string)
		covered := strings.Contains(card, "Tools named in the other columns may be absent too")
		for _, line := range strings.Split(card, "\n") {
			for _, n := range name.FindAllStringSubmatch(line, -1) {
				if _, ok := r.Lookup(n[1]); !ok || served(n[1]) || covered || strings.Contains(line, "`"+n[1]+"` (not on this server)") {
					continue
				}
				t.Errorf("group %s served alone: the card names %s, which the server does not serve, unmarked and with no line that says so: %s", g, n[1], line)
			}
		}
	}
}
