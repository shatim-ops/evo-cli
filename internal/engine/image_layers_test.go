// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// image и image_id — одна настройка из разных слоёв (профиль, .evo.yaml,
// окружение, флаги): BootImage читает тот образ, который задан в слое выше;
// оба в одном слое — CONFIG_INVALID (код 2) ещё до engine. Все ID выдуманы.

// layered resolves the configuration from a project file, the environment
// and flags, with no profile and no other CLOUDRU_* variable.
func layered(t *testing.T, projectYAML string, env map[string]string, flags map[string]any) (config.Config, error) {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "CLOUDRU_") {
			t.Setenv(k, "")
		}
	}
	t.Setenv(config.EnvConfigDir, t.TempDir())
	for k, v := range env {
		t.Setenv(k, v)
	}
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	if err := os.WriteFile(project, []byte(projectYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.ResolveWithSources(config.Options{ProjectFile: project, Flags: flags})
	return cfg, err
}

func TestBootImage_HigherLayerWins(t *testing.T) {
	for _, tc := range []struct {
		name   string
		yaml   string
		env    map[string]string
		flags  map[string]any
		wantID string
		byName bool // resolved through the catalogue search
	}{
		{"CLOUDRU_IMAGE_ID поверх image: в .evo.yaml", "image: Ubuntu-24.04\n",
			map[string]string{"CLOUDRU_IMAGE_ID": imgProd}, nil, imgProd, false},
		{"--image поверх CLOUDRU_IMAGE_ID", "",
			map[string]string{"CLOUDRU_IMAGE_ID": imgProd}, map[string]any{"Image": "ubuntu-24.04"}, imgUbuntu, true},
		{"--image-id поверх image: в .evo.yaml", "image: Ubuntu-24.04\n",
			nil, map[string]any{"ImageID": imgRedOS}, imgRedOS, false},
		{"только image: в .evo.yaml", "image: redos-7.3\n", nil, nil, imgRedOS, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := layered(t, tc.yaml, tc.env, tc.flags)
			if err != nil {
				t.Fatalf("config: %v", err)
			}
			if (cfg.Image != "") == (cfg.ImageID != "") {
				t.Fatalf("config keeps both or neither: image %q, image_id %q", cfg.Image, cfg.ImageID)
			}
			cloud := catalogForDeploy()
			searches := 0
			list := cloud.listImagesFn
			cloud.listImagesFn = func(ctx context.Context) ([]*provision.Image, error) {
				searches++
				return list(ctx)
			}
			eng, _ := newTestEngine(t, cloud, newMockDeployer())
			eng.Config.Image, eng.Config.ImageID = cfg.Image, cfg.ImageID
			img, err := eng.BootImage(context.Background(), eng.Config.ProjectID)
			if err != nil || img.ID != tc.wantID {
				t.Fatalf("BootImage: %v, image %+v, want %s", err, img, tc.wantID)
			}
			if (searches > 0) != tc.byName {
				t.Errorf("catalogue searched %d times, by name: %v", searches, tc.byName)
			}
		})
	}
}

func TestBootImage_SameLayerBothIsConfigInvalid(t *testing.T) {
	_, err := layered(t, "image: Ubuntu-24.04\nimage_id: "+imgUbuntu+"\n", nil, nil)
	if clerrors.Reason(err) != clerrors.CodeConfigInvalid || clerrors.ExitCode(err) != clerrors.ExitAuth ||
		!strings.Contains(err.Error(), "image_id") {
		t.Fatalf("оба в .evo.yaml: %v (exit %d)", err, clerrors.ExitCode(err))
	}
}
