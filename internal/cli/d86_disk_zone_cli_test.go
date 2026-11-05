// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// evo disk create without a zone (D86) is the command of a refusal that
// names zone, not region: at a terminal it asks for the zone as evo vm
// create does, and the call goes again with the zone chosen; without a
// terminal and with --yes the refusal stays, and its hint is the command's
// flag, --zone — not --region, which disk create has not, and not the
// tool's words. «Риск», заключение к #957.
func TestDiskCreate_NoZoneAskedAtTerminalAndHintedWithZoneFlag(t *testing.T) {
	fc := setupCLI(t)
	fc.zones = `[{"id":"` + pvZone + `","name":"ru.AZ-1","enabled":true}]`
	withCloud(t, noDisksCloud{previewCloud{}})
	t.Setenv("CLOUDRU_REGION", "")
	saveProfile(t, "default", "11111111-1111-1111-1111-111111111111")
	create := []string{"disk", "create", "data", "--size-gb", "20", "--dry-run"}

	for name, c := range map[string]struct {
		tty  bool
		args []string
	}{
		"no terminal": {false, create},
		"--yes":       {true, append([]string{"--yes"}, create...)},
	} {
		stdinIsTerminal = func() bool { return c.tty }
		code, _, stderr := runCLI(t, "\n\n", c.args...)
		prof, _, _, _ := config.LoadProfile("default")
		if code != clerrors.ExitAuth || strings.Contains(stderr, "Choose a zone") || prof.Region != "" {
			t.Errorf("%s: exit %d, region %q, want the refusal (2) and no question:\n%s", name, code, prof.Region, stderr)
		}
		if !strings.Contains(stderr, "Pass --zone (list: evo vm zones)") || strings.Contains(stderr, "Pass --region") {
			t.Errorf("%s: want the hint of --zone, the flag of disk create:\n%s", name, stderr)
		}
	}

	stdinIsTerminal = func() bool { return true }
	code, stdout, stderr := runCLI(t, "\n\n", create...)
	prof, _, _, _ := config.LoadProfile("default")
	if code != 0 || !strings.Contains(stdout+stderr, pvZone) || strings.Contains(stderr, "no availability zone is set") {
		t.Errorf("terminal: exit %d, want the preview in the zone chosen:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "Choose a zone (number, name or ID) [1]: ") || prof.Region != pvZone {
		t.Errorf("terminal: region %q, want ru.AZ-1 (%s) asked and saved:\n%s", prof.Region, pvZone, stderr)
	}
}
