// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Grok audit R5-07 (06.11, fixtures; release plan 1.8, #1095): a profile
// with project_id 12345 was taken without a word, and the error came later,
// from the cloud. project_id is a UUID (Config.ProjectID: «required
// UUID»; evo project use checks it): one that is not is CONFIG_INVALID at
// the load, naming the field and where it came from, before any request —
// from the profile, the project file or CLOUDRU_PROJECT_ID alike.
func TestResolve_ProjectIDNotAUUIDRefused(t *testing.T) {
	const bad = "12345"
	for _, layer := range []string{"profile", "project file", "env"} {
		t.Run(layer, func(t *testing.T) {
			t.Setenv(EnvConfigDir, filepath.Join(t.TempDir(), "evo"))
			t.Setenv("CLOUDRU_PROJECT_ID", "")
			t.Setenv("EVOLUTION_PROJECT_ID", "")
			t.Setenv("CLOUDRU_PROFILE", "")
			opts := Options{}
			switch layer {
			case "profile":
				if err := SaveProfile(DefaultProfile, Profile{ProjectID: bad}, Credentials{}); err != nil {
					t.Fatal(err)
				}
			case "project file":
				opts.ProjectFile = filepath.Join(t.TempDir(), ProjectFileName)
				if err := os.WriteFile(opts.ProjectFile, []byte("project_id: \""+bad+"\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "env":
				t.Setenv("CLOUDRU_PROJECT_ID", bad)
			}
			_, _, err := ResolveWithSources(opts)
			if clerrors.Reason(err) != clerrors.CodeConfigInvalid || !strings.Contains(errText(err), "project_id") && !strings.Contains(errText(err), "CLOUDRU_PROJECT_ID") {
				t.Errorf("project_id %q from the %s: %v; want CONFIG_INVALID naming the field", bad, layer, err)
			}
		})
	}
}
