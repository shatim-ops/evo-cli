// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// The install section of skills/README.md: what a person follows to put the
// scenario skills where their agent reads them. The owner's install of
// 24.10 (skills@1.7.4, tag v1.3.0) left Claude Code without the skills: the
// installer put them in ~/.agents/skills/ for the agents ticked, and Claude
// Code, not ticked, reads ~/.claude/skills/.

const skillsReadme = "skills/README.md"

// skillsInstallSection is the text of "## Установка" up to the next "## ".
func skillsInstallSection(t *testing.T) string {
	t.Helper()
	text := strings.ReplaceAll(string(mustReadRepo(t, skillsReadme)), "\r\n", "\n")
	i := strings.Index(text, "\n## Установка\n")
	if i < 0 {
		t.Fatalf("%s: no section «## Установка»", skillsReadme)
	}
	sec := text[i+1:]
	if j := strings.Index(sec[3:], "\n## "); j >= 0 {
		sec = sec[:j+4]
	}
	return sec
}

// skillsInstallPrompt is the first ```text block of the install section:
// the prompt a person gives their agent.
func skillsInstallPrompt(t *testing.T, sec string) string {
	t.Helper()
	i := strings.Index(sec, "```text\n")
	if i < 0 {
		t.Fatalf("%s: the install section has no ```text block with the prompt for the agent", skillsReadme)
	}
	body := sec[i+len("```text\n"):]
	j := strings.Index(body, "```")
	if j < 0 {
		t.Fatalf("%s: the prompt block is not closed", skillsReadme)
	}
	return body[:j]
}

// TestSkillsReadme_AskTheAgentFirst: the first way to install is a prompt
// for the agent, before any manual or installer way.
func TestSkillsReadme_AskTheAgentFirst(t *testing.T) {
	sec := skillsInstallSection(t)
	first := strings.Index(sec, "\n**")
	if first < 0 || !strings.HasPrefix(sec[first+1:], "**Попросите агента") {
		t.Errorf("%s: the first way in «Установка» must be «**Попросите агента…**»", skillsReadme)
	}
	prompt := strings.Index(sec, "```text\n")
	for _, other := range []string{"cp -r", "npx skills add"} {
		if k := strings.Index(sec, other); k >= 0 && (prompt < 0 || k < prompt) {
			t.Errorf("%s: %q comes before the prompt for the agent", skillsReadme, other)
		}
	}
}

// TestSkillsReadme_PromptSteps: the prompt installs from the tag of the
// person's evo only, copies the skills where the agent reads them, sets up
// the built-in skill, checks the version, and its evo command lines exist.
func TestSkillsReadme_PromptSteps(t *testing.T) {
	prompt := skillsInstallPrompt(t, skillsInstallSection(t))
	for _, want := range []string{
		"evo version --json",                    // the version decides the tag
		"git clone --branch v",                  // a tag, not a branch
		"--depth 1",                             // the tag alone
		"https://github.com/shatim-ops/evo-cli", // the official repository
		"`main`",                                // named as forbidden
		"skills/evo-*",                          // what is copied
		"~/.claude/skills/",                     // where Claude Code reads skills
		"evo agent setup --client",              // the built-in skill
		"--skill",
		"--dry-run",            // the diff before the write
		"metadata.evo-version", // the check after the copy
		"новую сессию",         // skills load at the start of a session
		"--toolsets all",       // evo-cleanup needs every group: the agent says so
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("%s: the prompt for the agent does not name %q", skillsReadme, want)
		}
	}
	root := newRootCmd()
	root.InitDefaultHelpCmd()
	invs := inlineInvocations(skillsReadme, prompt, 1)
	if len(invs) < 2 {
		t.Fatalf("%s: only %d evo command lines in the prompt", skillsReadme, len(invs))
	}
	for _, inv := range invs {
		for _, argv := range expandDocArgv(inv.Argv) {
			if err := checkDocArgv(root, argv, inv); err != nil {
				t.Errorf("%s, prompt: %s\n  %s", skillsReadme, err, inv.Text)
			}
		}
	}
}

// TestSkillsReadme_CheckInNewSession: after the install the person asks a
// new session which evo skills it sees, and the README says what to expect.
func TestSkillsReadme_CheckInNewSession(t *testing.T) {
	sec := skillsInstallSection(t)
	for _, want := range []string{"какие evo-скиллы ты видишь", "`evo`"} {
		if !strings.Contains(sec, want) {
			t.Errorf("%s: the check after the install does not name %q", skillsReadme, want)
		}
	}
}

// TestSkillsReadme_InstallerTickYourAgent: the skills installer installs only
// for the agents ticked; the README warns where it is described.
func TestSkillsReadme_InstallerTickYourAgent(t *testing.T) {
	sec := skillsInstallSection(t)
	i := strings.Index(sec, "npx skills add")
	if i < 0 {
		t.Skip("no skills installer in the README")
	}
	for _, want := range []string{"отметьте", "~/.agents/skills/"} {
		if !strings.Contains(sec[i:], want) {
			t.Errorf("%s: the installer way does not warn with %q", skillsReadme, want)
		}
	}
}

// TestSkillsReadme_DesktopNotChecked: the chat of Claude Desktop and
// claude.ai is named as not checked, with no steps made up.
func TestSkillsReadme_DesktopNotChecked(t *testing.T) {
	sec := skillsInstallSection(t)
	for _, want := range []string{"Claude Desktop", "claude.ai", "не проверено"} {
		if !strings.Contains(sec, want) {
			t.Errorf("%s: the install section does not name %q", skillsReadme, want)
		}
	}
}

// TestSkillsReadme_PromptClients: the clients the prompt lists are clients
// of 'evo agent setup', and a client without user-level rules (Cursor) is
// told to run the same command without --scope user.
func TestSkillsReadme_PromptClients(t *testing.T) {
	prompt := skillsInstallPrompt(t, skillsInstallSection(t))
	const lead = "(клиент: "
	i := strings.Index(prompt, lead)
	if i < 0 {
		t.Fatalf("%s: the prompt does not list the clients as %q", skillsReadme, lead+"…)")
	}
	list := prompt[i+len(lead):]
	list = list[:strings.Index(list, ")")]
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		if _, ok := lookupClient(name); !ok {
			t.Errorf("%s: the prompt lists %q, not a client of 'evo agent setup' (%s)", skillsReadme, name, strings.Join(clientNames(), ", "))
		}
	}
	for _, c := range agentClients {
		if _, user := c.skill[scopeUser]; user || !strings.Contains(list, c.name) {
			continue
		}
		if !strings.Contains(prompt, "ту же команду без `--scope user`") {
			t.Errorf("%s: %s has no user-level rules, and the prompt does not say to run the same command without `--scope user`", skillsReadme, c.name)
		}
	}
}
