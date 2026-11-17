// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"sort"
	"strings"
	"testing"
)

// echoedMarker stands for a password, a token or a key a user puts into the
// text of an argument a preview names (a template, a chain, a description).
const echoedMarker = "ECHOEDSECRET42"

// echoedShownAsGiven are the arguments of Decl.EchoedArgs a preview names as
// the call gives them, without a summary, with why that is no secret: a new
// one without a summary is declared here or gets one (the preview of
// image_create named the first line of a cloud-init template with a
// password, #2138: «Риск», conclusion 297, not a test).
var echoedShownAsGiven = map[string]string{
	"description":  "a note for people in the console, shown there to everyone with the project",
	"display_name": "the name the console shows, as public as the name",
	"lc_collate":   "a locale name of the database",
	"lc_ctype":     "a locale name of the database",
	"target_port":  "a port number",
}

// echoedTexts are the shapes of a text with a secret in it: on the first
// line, in a comment, in the body, in a header the summary may name.
func echoedTexts() []string {
	m := echoedMarker
	return []string{
		m,
		"#cloud-config password: " + m + "\nusers: []\n",
		"# token " + m + "\n#cloud-config\n",
		"#!/bin/sh " + m + "\necho hi\n",
		"#cloud-config\nchpasswd:\n  list: root:" + m + "\n",
		"## template: jinja " + m + "\n#cloud-config\n",
		"-----BEGIN CERTIFICATE-----\n" + m + "\n-----END CERTIFICATE-----\n",
		"-----BEGIN PRIVATE KEY-----\n" + m + "\n-----END PRIVATE KEY-----\n",
	}
}

// Every argument a preview names with a summary keeps its text out of it,
// whatever the text is: the preview is text that reaches a model, a log
// and a screen (CLAUDE.md §4: a secret reaches no output). The preview is
// one for MCP and --json, and the CLI shows its values
// (TestPreview_HumanShowsEveryValue), so the summary is the one point.
func TestEchoedArgs_SummaryKeepsTheTextOut(t *testing.T) {
	n := 0
	for _, tool := range sortedDecls() {
		for _, a := range decls[tool].EchoedArgs {
			if a.summary == nil {
				continue
			}
			n++
			for _, text := range echoedTexts() {
				if got := a.summary(text); strings.Contains(got, echoedMarker) {
					t.Errorf("%s %s: the summary names the text: %q → %q", tool, a.name, text, got)
				}
			}
		}
	}
	if n < 2 {
		t.Fatalf("arguments with a summary: %d — the check sees none", n)
	}
}

// namedArgs names an argument of a summary by the summary and nothing else
// — not the text beside it — for every tool that declares one.
func TestEchoedArgs_PreviewNamesTheSummaryOnly(t *testing.T) {
	preview := func(context.Context, map[string]any) (string, bool) {
		return `{"status":"confirmation_required","preview":{"params":[]}}`, true
	}
	for _, tool := range sortedDecls() {
		echoed := decls[tool].EchoedArgs
		for _, a := range echoed {
			if a.summary == nil {
				continue
			}
			for _, text := range echoedTexts() {
				got, _ := namedArgs(echoed, preview)(context.Background(), map[string]any{a.name: text})
				if strings.Contains(got, echoedMarker) {
					t.Errorf("%s %s: the preview shows the text: %q\n%s", tool, a.name, text, got)
				}
			}
		}
	}
}

// An argument a preview names as it is given is declared no secret, with
// why; a line here with no such argument left is dropped.
func TestEchoedArgs_ShownAsGivenAreDeclared(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range sortedDecls() {
		for _, a := range decls[tool].EchoedArgs {
			if a.summary != nil {
				continue
			}
			seen[a.name] = true
			if _, ok := echoedShownAsGiven[a.name]; !ok {
				t.Errorf("%s %s: the preview shows it as given, and it is not declared no secret (echoedShownAsGiven) — declare it with why, or give it a summary", tool, a.name)
			}
		}
	}
	for name := range echoedShownAsGiven {
		if !seen[name] {
			t.Errorf("echoedShownAsGiven names %s, no preview shows it as given: drop the line", name)
		}
	}
}

func sortedDecls() []string {
	names := make([]string, 0, len(decls))
	for name := range decls {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
