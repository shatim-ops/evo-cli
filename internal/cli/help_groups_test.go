// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// Корневая справка — по сценариям, а не по алфавиту (ревью r5 U5): у каждой
// команды корня есть группа, группы идут в порядке commandGroups, коды
// выхода — после списка команд и только в справке корня.
func TestRootHelp_EveryCommandHasGroup(t *testing.T) {
	root := newRootCmd()
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.IsAvailableCommand() || c.Name() == "help" {
			if c.GroupID == "" {
				t.Errorf("command %q has no group in commandGroups", c.Name())
			}
		}
	}
}

func TestRootHelp_GroupsInOrderAndExitCodesLast(t *testing.T) {
	setupCLI(t)
	code, stdout, _ := runCLI(t, "", "--help")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	last := -1
	for _, g := range commandGroups {
		i := strings.Index(stdout, g.en)
		if i < 0 {
			t.Fatalf("root help lacks group %q:\n%s", g.en, stdout)
		}
		if i < last {
			t.Errorf("group %q is out of order", g.en)
		}
		last = i
	}
	if strings.Contains(stdout, "Additional Commands:") {
		t.Errorf("a command fell out of the groups:\n%s", stdout)
	}
	codes := strings.Index(stdout, "Exit codes:")
	if codes < 0 || codes < strings.Index(stdout, "Flags:") {
		t.Errorf("exit codes must follow the commands and flags:\n%s", stdout)
	}

	_, sub, _ := runCLI(t, "", "vm", "--help")
	if strings.Contains(sub, "Exit codes:") {
		t.Errorf("exit codes belong to the root help only:\n%s", sub)
	}
}

// Корневая справка заканчивается приветствием (поручение владельца 30.10,
// rc.2 1.6, п. 28), а не ссылками на Telegram.
func TestRootHelp_Welcome(t *testing.T) {
	long := newRootCmd().Long
	if !strings.HasSuffix(long, "Добро пожаловать в Cloud.ru") && !strings.HasSuffix(long, "Welcome to Cloud.ru") {
		t.Errorf("корневая справка не заканчивается приветствием: %q", long)
	}
	if strings.Contains(long, "t.me/") {
		t.Errorf("в корневой справке ссылка на Telegram: %q", long)
	}
	// И в тексте evo version: обратная связь — Issues (README); поле
	// feedback в --json остаётся до 2.0 (#686).
	if crew := crewText(); strings.Contains(crew, "t.me/") {
		t.Errorf("в тексте evo version ссылка на Telegram: %q", crew)
	}
}
