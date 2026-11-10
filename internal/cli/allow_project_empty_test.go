// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// Grok audit R5-21 (06.11, fixtures; release plan 1.8, #1095): a value of
// --allow-project that is no project («garbage», «,») closes the server —
// «project … is not allowed» — and an empty one (--allow-project=, a
// template that put an empty variable into the command of an HTTP
// deployment) opened it to every project the key sees, without a word.
// An empty --allow-project is refused at the start, as the others are
// refused at the call: the limit the person meant to set is not dropped.
func TestMCP_EmptyAllowProjectRefused(t *testing.T) {
	for _, args := range [][]string{
		{"mcp", "--allow-project="},
		{"mcp", "--allow-project", ""},
	} {
		code, stdout, stderr := runCLI(t, "", args...)
		if code == 0 || !strings.Contains(stdout+stderr, "--allow-project") {
			t.Errorf("%q: exit %d — an empty --allow-project must refuse the start, naming the flag\n%s\n%s", args, code, stdout, stderr)
		}
	}
}

// D89 (conclusion 38 of «Риск»): one rule for the flags whose empty value
// meant «no flag» — --allow-project (the limit of projects), --profile
// (another key and project, without a word), --toolsets: given empty, it
// is USAGE before anything starts, naming the flag. Not given — as before.
func TestEmptyExplicitFlagIsUsage(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "mcp", "--allow-project="},
		{"--json", "mcp", "--toolsets="},
		{"--json", "mcp", "--toolsets", " "},
		{"--json", "--profile=", "whoami"},
		{"--json", "whoami", "--profile", ""},
		{"--json", "--profile=", "mcp"},
		// The owner's decision 10.11 (conclusion 53): the scope flags too —
		// an empty one moved the write to the project or zone of the profile.
		{"--json", "--project-id=", "vm", "list"},
		{"--json", "vm", "list", "--region", ""},
		{"--json", "disk", "create", "d1", "--size-gb", "10", "--zone="},
		{"--json", "disk", "create", "d1", "--size-gb", "10", "--project-id="},
		{"--json", "deploy", "--region="},
		{"--json", "configure", "--region="},
	} {
		code, stdout, stderr := runCLI(t, "", args...)
		flag := ""
		for _, a := range args {
			if strings.HasPrefix(a, "--") && a != "--json" {
				flag, _, _ = strings.Cut(a, "=")
			}
		}
		if code != 1 || !strings.Contains(stdout+stderr, `"USAGE"`) || !strings.Contains(stdout+stderr, flag) {
			t.Errorf("%q: exit %d, want 1 USAGE naming %s\n%s%s", args, code, flag, stdout, stderr)
		}
	}
}
