// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
	"os"
	"syscall"
)

// linkCount returns the number of hard links to the open file whose Stat is
// st, or 0 when the file system does not say.
func linkCount(_ *os.File, st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Nlink) // uint16 on darwin, uint64 on linux/amd64
	}
	return 0
}
