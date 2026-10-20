// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package cli

import (
	"os"
	"syscall"
)

// linkCount returns the number of hard links to the open file f, or 0 when
// the file system does not say (GetFileInformationByHandle fails, e.g. on
// some network shares).
func linkCount(f *os.File, _ os.FileInfo) uint64 {
	var d syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &d); err != nil {
		return 0
	}
	return uint64(d.NumberOfLinks)
}
