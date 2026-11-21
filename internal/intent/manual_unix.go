// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package intent

import (
	"github.com/shatim-ops/evo-cli/internal/config"
	"os"
)

func openManualReceipt(path string) (*os.File, error) {
	return config.OpenNoFollow(path, os.O_RDONLY, 0)
}
func publishManualDirectory(stage, final string) error { return os.Rename(stage, final) }
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
