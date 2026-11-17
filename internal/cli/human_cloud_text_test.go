// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// cloudProbe is a name from the cloud with a line and a paragraph
// separator, a bidi override and a zero-width space in it.
const cloudProbe = "PROBE\u2028LINE\u2029END\u202eBIDI\u200bZERO"

func hasProbeChars(s string) bool { return strings.ContainsAny(s, "\u2028\u2029\u202e\u200b") }

// A name the cloud gives goes through output.SanitizeCell wherever the
// human output prints it, not only in a table: the project evo configure
// takes or offers, and the version in the header of an addon's settings
// schema (Astra, #2024, the paths next to #1950). Checked on what the
// command prints.
func TestHumanOutput_CloudNamesAreSanitized(t *testing.T) {
	fc := setupCLI(t)
	fc.orgProject = `[{"id":"11111111-1111-1111-1111-111111111111","name":"` + cloudProbe + `","status":"active"}]`
	code, stdout, stderr := runCLI(t, fakeSecret+"\n", "configure", "--key-id", fakeKeyID, "--secret-stdin")
	if code != 0 || !strings.Contains(stdout+stderr, "PROBE") || hasProbeChars(stdout+stderr) {
		t.Errorf("configure, one project: exit %d, the name printed raw or not at all: %q", code, stdout+stderr)
	}

	fc.orgProject = `[{"id":"p-1","name":"` + cloudProbe + `"},{"id":"p-2","name":"two"}]`
	var menu bytes.Buffer
	con := newConsole(strings.NewReader("1\n"), &menu, true)
	if _, err := chooseProject(context.Background(), "token", "", con, output.NewWithWriter(&menu, output.ModePlain, false)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(menu.String(), "PROBE") || hasProbeChars(menu.String()) {
		t.Errorf("the project menu prints the name raw: %q", menu.String())
	}

	// The command an image card offers names an image whose name the
	// terminal would not print as it is by its UUID.
	var card bytes.Buffer
	img := `{"image":{"id":"00000000-0000-4000-8000-0000000000c1","name":"` + cloudProbe + `"},"ready":true,"fields_required":["` + cloudProbe + `"]}`
	if err := renderImageCard(output.NewWithWriter(&card, output.ModePlain, false), &card, img); err != nil {
		t.Fatal(err)
	}
	if hasProbeChars(card.String()) || !strings.Contains(card.String(), "--image 00000000-0000-4000-8000-0000000000c1") {
		t.Errorf("the image card prints the name raw or offers it as a command: %q", card.String())
	}

	var addon bytes.Buffer
	answer := `{"addon":"probe","releases":[{"version":"` + cloudProbe + `","configuration_schema":"{\"a\":1}"}]}`
	if err := renderAddonVersions(output.NewWithWriter(&addon, output.ModePlain, false), &addon, answer); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(addon.String(), "PROBE") || hasProbeChars(addon.String()) {
		t.Errorf("addon-versions --full prints the version raw: %q", addon.String())
	}
}
