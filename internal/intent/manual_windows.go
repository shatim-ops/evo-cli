// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"

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

// A read-only snapshot holds its original descriptor briefly. Only the
// authenticated terminal path converts it to a private hard link.
type manualSource struct {
	path      string
	id        manualFileID
	file      *os.File
	pin       string
	parent    *os.File
	parentID  manualFileID
	stageDirs []*os.File
}

func (s *manualSource) Name() string { return s.path }

func (s *manualSource) Close() error {
	if s.parent != nil {
		s.parent.Close()
		s.parent = nil
	}
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
	defer func() { closeManualDirs(s.stageDirs); s.stageDirs = nil }()
	if s.pin == "" {
		return nil
	}
	if err := s.removePin(); err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("Cleanup of temporary receipt link %q failed", "Не удалось убрать временную ссылку квитанции %q", s.pin), err)
	}
	s.pin = ""
	return nil
}

func readManualSource(path string) (*manualSource, []byte, error) {
	dirs, err := holdManualDirs(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	defer closeManualDirs(dirs)
	// Pin the directory identity without denying a future rename. Check every
	// path component only during acquisition, not during the operator prompt.
	raw, err := windows.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	h, err := windows.CreateFile(raw, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, nil, err
	}
	parent := os.NewFile(uintptr(h), filepath.Dir(path))
	keepParent := false
	defer func() {
		if !keepParent {
			parent.Close()
		}
	}()
	parentID, err := manualID(parent)
	if err != nil {
		return nil, nil, err
	}
	f, err := openManualReceipt(path)
	if err != nil {
		return nil, nil, err
	}
	id, err := manualID(f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	keepParent = true
	return &manualSource{path: path, id: id, file: f, parent: parent, parentID: parentID}, data, nil
}

func (s *manualSource) matches(other *manualSource) (bool, error) {
	return s.id == other.id && s.parentID == other.parentID, nil
}
func (s *manualSource) matchesFile(f *os.File) (bool, error) {
	id, err := manualID(f)
	return s.id == id, err
}

// Keep every path component in place while operating by pathname. Open the
// reparse point itself and reject it, including junctions in ancestor paths.
// These are directory handles; they allow the old writer to replace files.
func holdManualDirs(path string) (dirs []*os.File, err error) {
	volume := filepath.VolumeName(path)
	if !filepath.IsAbs(path) || len(volume) != 2 {
		return nil, os.ErrInvalid
	}
	current := volume + string(os.PathSeparator)
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), current), string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		name, e := windows.UTF16PtrFromString(current)
		if e != nil {
			closeManualDirs(dirs)
			return nil, e
		}
		h, e := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if e != nil {
			closeManualDirs(dirs)
			return nil, &os.PathError{Op: "open", Path: current, Err: e}
		}
		f := os.NewFile(uintptr(h), current)
		dirs = append(dirs, f)
		var info windows.ByHandleFileInformation
		e = windows.GetFileInformationByHandle(h, &info)
		if e != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			closeManualDirs(dirs)
			return nil, os.ErrPermission
		}
	}
	if len(dirs) == 0 {
		return nil, os.ErrPermission
	}
	return dirs, nil
}
func closeManualDirs(dirs []*os.File) {
	for i := len(dirs) - 1; i >= 0; i-- {
		dirs[i].Close()
	}
}

// Check owner and effective DACL from the handle. Missing, unreadable and
// unfamiliar security descriptors fail closed. Never change the shared ACL.
func checkManualACL(f *os.File, private bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	trusted := func(sid *windows.SID) bool {
		return sid != nil && (sid.Equals(user.User.Sid) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || !trusted(owner) {
		return os.ErrPermission
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return os.ErrPermission
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return os.ErrPermission
		}
		if trusted((*windows.SID)(unsafe.Pointer(&ace.SidStart))) {
			continue
		}
		const deleteChild = 0x40
		const writes = windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | deleteChild | windows.FILE_WRITE_ATTRIBUTES | windows.FILE_WRITE_EA | windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE | windows.GENERIC_WRITE | windows.GENERIC_ALL
		if private || ace.Mask&writes != 0 {
			return os.ErrPermission
		}
	}
	return nil
}

func (s *manualSource) prepareTerminal() (string, error) {
	if s.file == nil {
		return "", os.ErrInvalid
	}
	if err := checkManualACL(s.file, true); err != nil {
		return "", err
	}
	sourceDirs, err := holdManualDirs(filepath.Dir(s.path))
	if err != nil {
		return "", err
	}
	defer closeManualDirs(sourceDirs)
	dirID, err := manualID(sourceDirs[len(sourceDirs)-1])
	if err != nil || dirID != s.parentID {
		return "", os.ErrInvalid
	}
	// Link from a verified handle, so swapping the original pathname cannot
	// retain a different receipt even transiently.
	name, err := windows.UTF16PtrFromString(s.path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(name, windows.FILE_WRITE_ATTRIBUTES|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", err
	}
	guard := os.NewFile(uintptr(h), s.path)
	defer guard.Close()
	same, err := s.matchesFile(guard)
	if err != nil || !same {
		return "", os.ErrInvalid
	}
	base, err := holdManualDirs(config.Dir())
	if err != nil {
		return "", err
	}
	defer closeManualDirs(base)
	if err = checkManualACL(base[len(base)-1], false); err != nil {
		return "", err
	}
	stage := filepath.Join(config.Dir(), "intent-review")
	if err = config.MkdirPrivate(stage); err != nil {
		return "", err
	}
	dirs, err := holdManualDirs(stage)
	if err != nil {
		return "", err
	}
	s.stageDirs = dirs
	if err = checkManualACL(dirs[len(dirs)-1], true); err != nil {
		return "", err
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return "", err
	}
	path := filepath.Join(stage, "review-"+hex.EncodeToString(token[:])+".receipt")
	if err = linkPreviewHandle(h, windows.Handle(dirs[len(dirs)-1].Fd()), filepath.Base(path)); err != nil {
		return "", err
	}
	s.pin = path
	if err = s.verifyPin(); err != nil {
		return "", err
	}
	err = s.file.Close()
	s.file = nil
	return path, err
}

func (s *manualSource) verifyPin() error {
	if s.pin == "" {
		return os.ErrInvalid
	}
	f, err := openManualReceipt(s.pin)
	if err != nil {
		return err
	}
	defer f.Close()
	same, err := s.matchesFile(f)
	if err != nil {
		return err
	}
	if !same {
		return os.ErrInvalid
	}
	return checkManualACL(f, true)
}
func (s *manualSource) linkDecision(path string) error {
	if err := s.verifyPin(); err != nil {
		return err
	}
	return os.Link(s.pin, path)
}

// Remove by handle so a replaced name cannot redirect cleanup to a new file.
func (s *manualSource) removePin() error {
	name, err := windows.UTF16PtrFromString(s.pin)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(name, windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(h), s.pin)
	defer f.Close()
	same, err := s.matchesFile(f)
	if err != nil {
		return err
	}
	if !same {
		return os.ErrInvalid
	}
	// Remove this link with POSIX semantics even while a short reader still
	// holds the object. Unsupported filesystems return an explicit cleanup error.
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS)
	return windows.SetFileInformationByHandle(h, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), 4)
}

// FileLinkInformation (MS-FSCC) is the handle-relative operation used by
// Go's Windows Root.Link. ReplaceIfExists is zero, and the kernel takes the
// destination relative to the verified directory rather than resolving its path.
func linkPreviewHandle(src, dir windows.Handle, name string) error {
	var info struct {
		Replace   uint32
		Directory windows.Handle
		Bytes     uint32
		Name      [64]uint16
	}
	u, err := windows.UTF16FromString(name)
	if err != nil {
		return err
	}
	if len(u) > len(info.Name) {
		return os.ErrInvalid
	}
	info.Directory = dir
	info.Bytes = uint32(2 * (len(u) - 1))
	copy(info.Name[:], u)
	const fileLinkInformation = 11
	return windows.NtSetInformationFile(src, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), fileLinkInformation)
}
