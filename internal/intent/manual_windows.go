// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"golang.org/x/sys/windows"
	"os"
)

// Hold the previewed object without denying rename or delete to older evo processes.
// OPEN_REPARSE_POINT opens the link itself; reject it and every non-regular object.
func openManualReceipt(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path)
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		f.Close()
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		f.Close()
		return nil, os.ErrInvalid
	}
	return f, nil
}

// The decision file is flushed before publication. The directory move uses
// WRITE_THROUGH, without replacement or cross-volume copying. Readers also
// require the original on-disk anchor and the complete decision file.
func publishManualDirectory(stage, final string) error {
	from, err := windows.UTF16PtrFromString(stage)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(final)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

// Directory publication on Windows uses MoveFileEx with WRITE_THROUGH.
func syncDirectory(string) error { return nil }
