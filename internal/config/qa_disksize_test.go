// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// QA-14 (QA 20.10): --disk-size -5 молча становилось 10 ГБ. Отрицательный
// размер из флага — USAGE (код 1), из CLOUDRU_DISK_SIZE — CONFIG_INVALID
// (код 2); 0 по-прежнему значит «не задан».
func TestDiskSize_NegativeRefused(t *testing.T) {
	isolate(t)
	if _, err := resolveConfig(Options{Flags: map[string]any{"DiskSize": -5}}); exitCode(err) != clerrors.ExitGeneral {
		t.Errorf("flag -5: %v (exit %d)", err, exitCode(err))
	}
	t.Setenv("CLOUDRU_DISK_SIZE", "-5")
	if _, err := resolveConfig(Options{}); exitCode(err) != clerrors.ExitAuth {
		t.Errorf("env -5: %v (exit %d)", err, exitCode(err))
	}
	t.Setenv("CLOUDRU_DISK_SIZE", "")
	cfg, err := resolveConfig(Options{Flags: map[string]any{"DiskSize": 0}})
	if err != nil || cfg.DiskSize != 0 {
		t.Errorf("flag 0: %v %d", err, cfg.DiskSize)
	}
}
