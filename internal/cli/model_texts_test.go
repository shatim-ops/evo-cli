// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// Тексты для модели (P10, решение владельца 31.10). evo mcp без --toolsets
// отдаёт клиенту все инструменты в каждой сессии (~30 тыс. токенов на
// запрос в прогоне Grok с LLM): сервер говорит это при запуске, по stdio
// тоже — в stderr, который не канал протокола.
func TestMCP_WarnsWithoutToolsets(t *testing.T) {
	setupCLI(t)
	_, stdout, stderr := runCLI(t, "", "mcp")
	if !strings.Contains(stderr, "no --toolsets") || !strings.Contains(stderr, "KB") {
		t.Errorf("evo mcp without --toolsets does not warn about the size of tools/list; stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, "--toolsets") {
		t.Errorf("the warning went to stdout, the protocol channel:\n%s", stdout)
	}
	_, _, stderr = runCLI(t, "", "mcp", "--toolsets", "vm")
	if strings.Contains(stderr, "no --toolsets") {
		t.Errorf("evo mcp --toolsets vm warns about --toolsets; stderr:\n%s", stderr)
	}
}

// --yes is the person's consent given in advance; in the LLM run the
// model added it itself in 2 to 5 tasks of 7 (G2-7).
func TestYesFlag_IsThePersonsConsent(t *testing.T) {
	f := newRootCmd().PersistentFlags().Lookup("yes")
	if f == nil {
		t.Fatal("no --yes")
	}
	if !strings.Contains(f.Usage, "person") {
		t.Errorf("--yes does not say whose consent it is: %q", f.Usage)
	}
}

// The rules for the agent send a task that only reads to a read-only
// server (G2-17: a FinOps task went on to delete).
func TestAgentSkill_ReadOnlyTask(t *testing.T) {
	_, stdout, _ := runCLI(t, "", "agent", "skill")
	if !strings.Contains(stdout, "only reads") || !strings.Contains(stdout, "--read-only") {
		t.Errorf("the rules do not send a read-only task to evo mcp --read-only:\n%s", stdout)
	}
}
