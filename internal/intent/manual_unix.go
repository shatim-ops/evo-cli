// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package intent

import (
	"io"
	"os"
	"path/filepath"

	"github.com/shatim-ops/evo-cli/internal/config"
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

// POSIX permits an older writer to replace the name while this descriptor pins the inode.
type manualSource struct{ *os.File }

func readManualSource(path string) (*manualSource, []byte, error) {
	f, err := openManualReceipt(path)
	if err != nil {
		return nil, nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return &manualSource{f}, data, nil
}
func (s *manualSource) matches(other *manualSource) (bool, error) { return s.matchesFile(other.File) }
func (s *manualSource) matchesFile(f *os.File) (bool, error) {
	a, err := s.Stat()
	if err != nil {
		return false, err
	}
	b, err := f.Stat()
	if err != nil {
		return false, err
	}
	return os.SameFile(a, b), nil
}

func (s *manualSource) prepareTerminal() (string, error) { return "", nil }
func (s *manualSource) linkDecision(path string) error   { return os.Link(s.Name(), path) }

func canonicalManualDir(path string) (string, error) { return filepath.EvalSymlinks(path) }
