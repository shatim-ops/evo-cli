// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// OpenNoFollow opens a file evo keeps in its configuration directory or a
// project's .evo/ only as itself: a symlink or another non-regular file put
// in its place — committed to a repository, it comes with git clone — is
// an error, not a way to write, create or read the file it points to
// (findings ledger 1.8, № 29; R-67, «Риск», conclusion 61). O_NOFOLLOW
// refuses a link at the open where there is one; the file opened is then
// checked to be the one Lstat saw. A file rewritten whole goes through
// WriteFileAtomic instead: its rename replaces a link.
func OpenNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	before, lerr := os.Lstat(path)
	if lerr == nil && !before.Mode().IsRegular() {
		return nil, notRegular(path)
	}
	f, err := os.OpenFile(path, flag|oNoFollow, perm)
	if err != nil {
		if after, aerr := os.Lstat(path); aerr == nil && !after.Mode().IsRegular() {
			return nil, notRegular(path)
		}
		return nil, err
	}
	if lerr == nil {
		if got, err := f.Stat(); err != nil || !os.SameFile(before, got) {
			f.Close()
			return nil, notRegular(path)
		}
	}
	return f, nil
}

func notRegular(path string) error {
	return fmt.Errorf(i18n.T("%s is not a regular file (a symlink?): evo does not write or read through it; delete it",
		"%s — не обычный файл (символьная ссылка?): evo не пишет и не читает через него; удалите его"), path)
}
