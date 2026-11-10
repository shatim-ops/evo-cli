// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"
	"testing"
)

// Findings ledger 1.8, № 44 (R5-10): CLOUDRU_CONFIG_DIR set empty read the
// default directory with no word of it — a script that meant to isolate
// its run worked with the person's profiles. The configuration's warnings
// (stderr, doctor) say so with the path used.
func TestResolve_EmptyConfigDirIsNamed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	for _, empty := range []bool{false, true} {
		if empty {
			t.Setenv(EnvConfigDir, "")
		} else {
			t.Setenv(EnvConfigDir, home+"/evo")
		}
		cfg, _, err := ResolveWithSources(Options{})
		if err != nil {
			t.Fatal(err)
		}
		said := false
		for _, w := range cfg.Warnings {
			if strings.Contains(w, EnvConfigDir) && strings.Contains(w, Dir()) {
				said = true
			}
		}
		if said != empty {
			t.Errorf("empty %v: warnings %q", empty, cfg.Warnings)
		}
	}
}
