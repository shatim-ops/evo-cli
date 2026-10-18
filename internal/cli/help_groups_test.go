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
