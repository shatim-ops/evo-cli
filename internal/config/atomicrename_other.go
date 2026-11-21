// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

// Only Windows blocks a rename over a file open for reading; elsewhere a
// rename is not refused for a concurrent reader.
func renameBlockedByReader(error) bool { return false }

func publishDurableReceipt(tmp, path string) error {
	if err := renameAtomic(tmp, path); err != nil {
		return err
	}
	// Publish both the file entry and the per-operation directory entry.
	dir := filepath.Dir(path)
	for _, name := range []string{dir, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))} {
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		err = f.Sync()
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
