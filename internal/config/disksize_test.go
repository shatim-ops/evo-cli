// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	stderrors "errors"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func reasonOf(err error) string {
	var ce *clerrors.CLIError
	if stderrors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// QA-14: --disk-size -5 silently became 10 GB. A negative size is USAGE
// from a flag (or a tool argument, which comes as one), CONFIG_INVALID from
// the environment or .evo.yaml; 0 is unset, the default.
func TestResolve_NegativeDiskSizeRefused(t *testing.T) {
	isolate(t)
	for _, v := range []any{-5, "-5", float64(-1)} {
		_, _, err := ResolveWithSources(Options{Flags: map[string]any{"DiskSize": v}})
		if reasonOf(err) != clerrors.CodeUsage || !strings.Contains(err.Error(), "-") {
			t.Errorf("flag %v: err = %v, want USAGE", v, err)
		}
		if err != nil && strings.Contains(err.Error(), "flag DiskSize") {
			t.Errorf("flag %v: the internal field name in the message: %v", v, err)
		}
	}
	for _, v := range []any{0, 20} {
		cfg, _, err := ResolveWithSources(Options{Flags: map[string]any{"DiskSize": v}})
		if err != nil || (v == 20 && cfg.DiskSize != 20) {
			t.Errorf("flag %v: %+v, %v", v, cfg.DiskSize, err)
		}
	}

	t.Setenv("CLOUDRU_DISK_SIZE", "-3")
	_, _, err := ResolveWithSources(Options{})
	if reasonOf(err) != clerrors.CodeConfigInvalid || !strings.Contains(err.Error(), "CLOUDRU_DISK_SIZE") || strings.Contains(err.Error(), "USAGE") {
		t.Errorf("env: err = %v, want CONFIG_INVALID naming the variable", err)
	}
	t.Setenv("CLOUDRU_DISK_SIZE", "")

	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "disk_size: -2\n")
	_, _, err = ResolveWithSources(Options{ProjectFile: project})
	if reasonOf(err) != clerrors.CodeConfigInvalid || !strings.Contains(err.Error(), "disk_size") {
		t.Errorf(".evo.yaml: err = %v, want CONFIG_INVALID naming disk_size", err)
	}
	writeFile(t, project, "disk_size: 0\n")
	if cfg, _, err := ResolveWithSources(Options{ProjectFile: project}); err != nil || cfg.DiskSize != Defaults().DiskSize {
		t.Errorf(".evo.yaml disk_size 0: %d, %v; want the default", cfg.DiskSize, err)
	}
}
