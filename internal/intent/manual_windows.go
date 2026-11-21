// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Open the receipt only for the short read and identity check.
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

// FILE_ID_INFO identifies the open object on its volume. Capture it from the
// handle used for the bytes, never by statting the pathname after a rename.
type manualFileID struct {
	Volume uint64
	ID     [16]byte
}

func manualID(f *os.File) (manualFileID, error) {
	var id manualFileID
	raw, err := f.SyscallConn()
	if err != nil {
		return id, err
	}
	var queryErr error
	err = raw.Control(func(fd uintptr) {
		queryErr = windows.GetFileInformationByHandleEx(windows.Handle(fd), windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id)))
	})
	if err != nil {
		return id, err
	}
	return id, queryErr
}

// An older MoveFileEx writer needs the destination closed while the operator
// thinks. Watch directory entry changes from before the read. Never reset the
// signal: even deletion and reuse of the same file ID invalidates the preview.
type manualSource struct {
	path  string
	id    manualFileID
	watch windows.Handle
}

func (s *manualSource) Name() string { return s.path }
func (s *manualSource) Close()       { windows.FindCloseChangeNotification(s.watch) }
func (s *manualSource) unchanged() error {
	status, err := windows.WaitForSingleObject(s.watch, 0)
	if err != nil {
		return err
	}
	if status != uint32(windows.WAIT_TIMEOUT) {
		return manualError("The receipt directory changed. Run intent close again and review the receipt.", "Каталог квитанций изменился. Снова запустите intent close и проверьте квитанцию.")
	}
	return nil
}
func readManualSource(path string) (*manualSource, []byte, error) {
	watch, err := windows.FindFirstChangeNotification(filepath.Dir(path), false, windows.FILE_NOTIFY_CHANGE_FILE_NAME)
	if err != nil {
		return nil, nil, err
	}
	s := &manualSource{path: path, watch: watch}
	ok := false
	defer func() {
		if !ok {
			s.Close()
		}
	}()
	f, err := openManualReceipt(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	s.id, err = manualID(f)
	if err != nil {
		return nil, nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	if err = s.unchanged(); err != nil {
		return nil, nil, err
	}
	ok = true
	return s, data, nil
}
func (s *manualSource) matches(other *manualSource) (bool, error) {
	if err := other.unchanged(); err != nil {
		return false, err
	}
	return s.id == other.id, s.unchanged()
}
func (s *manualSource) matchesFile(f *os.File) (bool, error) {
	id, err := manualID(f)
	if err != nil {
		return false, err
	}
	return s.id == id, s.unchanged()
}
