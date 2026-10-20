// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// insideWorkDir checks that path, after symlinks, lies inside the server's
// working directory. Over HTTP the caller is remote: a tool must not read
// files of the server's host outside the project it serves.
func insideWorkDir(path string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(wd)
	if err != nil {
		return err
	}
	p := path
	if !filepath.IsAbs(p) {
		p = filepath.Join(wd, p)
	}
	// A missing file is reported by the tool itself; resolve the part of
	// the path that exists (its nearest existing ancestor).
	p = filepath.Clean(p)
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = filepath.Join(r, rest)
			break
		}
		parent := filepath.Dir(p)
		if parent == p {
			p = filepath.Join(p, rest)
			break
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
	rel, err := filepath.Rel(root, filepath.Clean(p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%q is outside the server's working directory", path)
	}
	return nil
}
