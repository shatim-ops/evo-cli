// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Windows has no file modes: os.Chmod toggles only the read-only bit, and a
// new file or directory inherits the ACL of its parent. In a project folder
// made in the root of a drive (C:\work) that ACL lets every authenticated
// user read and change files. Secrets and the directories holding them are
// therefore created with an explicit, protected DACL (no inheritance) that
// grants full access to three principals only:
//
//   - the current user;
//   - SYSTEM — services, backup and the OS itself act as SYSTEM; locking it
//     out protects nothing and breaks backup and antivirus;
//   - BUILTIN\Administrators — an administrator can take ownership of any
//     file anyway (SeTakeOwnershipPrivilege), so excluding them is no
//     barrier, only an obstacle to administration.
//
// This is the set OpenSSH for Windows accepts for ~/.ssh and private keys
// (anything else, e.g. "Authenticated Users" or "Everyone", makes ssh refuse
// the key as "too open") and what its Repair-*Permission scripts set.

// Access rights that matter for a secret: reading it, changing it, or
// changing who may do either.
const (
	accessDelete      = 0x00010000
	accessDeleteChild = 0x00000040 // FILE_DELETE_CHILD
	fileReadOrWrite   = windows.FILE_READ_DATA | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA |
		windows.WRITE_DAC | windows.WRITE_OWNER | accessDelete |
		windows.GENERIC_READ | windows.GENERIC_WRITE | windows.GENERIC_ALL
	// In a directory FILE_WRITE_DATA is "add file", FILE_APPEND_DATA "add
	// subdirectory": whoever has them can plant or replace files there.
	dirWrite = windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | accessDeleteChild |
		windows.WRITE_DAC | windows.WRITE_OWNER | accessDelete |
		windows.GENERIC_WRITE | windows.GENERIC_ALL
)

// privateSDDL is the security descriptor of a private file or directory:
// "D:P" is a protected DACL, OICI makes files and subdirectories created in
// a directory inherit the same three entries.
func privateSDDL(dir bool) (string, error) {
	user, err := currentUserSID()
	if err != nil {
		return "", err
	}
	flags := ""
	if dir {
		flags = "OICI"
	}
	return fmt.Sprintf("D:P(A;%[1]s;FA;;;%[2]s)(A;%[1]s;FA;;;SY)(A;%[1]s;FA;;;BA)", flags, user), nil
}

func currentUserSID() (string, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf(i18n.T("current user: %w", "текущий пользователь: %w"), err)
	}
	return tu.User.Sid.String(), nil
}

// currentUserName is DOMAIN\user of the process, for icacls hints (cmd
// and PowerShell expand %USERNAME% differently, a literal name works in both).
func currentUserName() string {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "%USERNAME%"
	}
	return accountName(tu.User.Sid)
}

// accountName is DOMAIN\name of a SID, or the SID string when it has none.
func accountName(sid *windows.SID) string {
	acc, dom, _, err := sid.LookupAccount("")
	if err != nil {
		return sid.String()
	}
	if dom != "" {
		return dom + `\` + acc
	}
	return acc
}

func privateAttributes(dir bool) (*windows.SecurityAttributes, error) {
	sddl, err := privateSDDL(dir)
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("security descriptor: %w", "дескриптор безопасности: %w"), err)
	}
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	return sa, nil
}

// MkdirPrivate is os.MkdirAll(dir, 0o700): every directory it creates gets
// the private DACL. An existing directory is left as it is.
func MkdirPrivate(dir string) error {
	if st, err := os.Stat(dir); err == nil {
		if st.IsDir() {
			return nil
		}
		return &os.PathError{Op: "mkdir", Path: dir, Err: syscall.ENOTDIR}
	}
	if parent := filepath.Dir(dir); parent != dir {
		if err := MkdirPrivate(parent); err != nil {
			return err
		}
	}
	sa, err := privateAttributes(true)
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	if err := windows.CreateDirectory(p, sa); err != nil {
		// Made by someone else in the meantime: fine if it is a directory.
		if st, serr := os.Stat(dir); serr == nil && st.IsDir() {
			return nil
		}
		return &os.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	return nil
}

// createPrivateTemp is os.CreateTemp whose file carries the private DACL
// from the moment it exists (SECURITY_ATTRIBUTES of CreateFile), not after
// a later SetSecurityInfo.
func createPrivateTemp(dir, pattern string) (*os.File, error) {
	sa, err := privateAttributes(false)
	if err != nil {
		return nil, &os.PathError{Op: "createtemp", Path: filepath.Join(dir, pattern), Err: err}
	}
	prefix, suffix := pattern, ""
	if i := strings.LastIndex(pattern, "*"); i >= 0 {
		prefix, suffix = pattern[:i], pattern[i+1:]
	}
	for range 10000 {
		name := filepath.Join(dir, prefix+strconv.FormatUint(uint64(rand.Uint32()), 10)+suffix)
		p, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, &os.PathError{Op: "createtemp", Path: name, Err: err}
		}
		h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, sa,
			windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return nil, &os.PathError{Op: "createtemp", Path: name, Err: err}
		}
		return os.NewFile(uintptr(h), name), nil
	}
	return nil, &os.PathError{Op: "createtemp", Path: filepath.Join(dir, pattern), Err: os.ErrExist}
}

// RestrictToOwner replaces the DACL of an existing file or directory with
// the private one (the Windows counterpart of chmod 0600/0700). For a
// directory the new entries also reach the files inside that inherit.
func RestrictToOwner(path string, dir bool) error {
	sddl, err := privateSDDL(dir)
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// ForeignAccess lists who besides the current user, SYSTEM and
// Administrators may read or change the file (dir=false), or add, replace
// and delete files in the directory (dir=true). An inherited DACL counts
// with what it actually grants: the one of %AppData% names nobody else.
// Nil when the ACL cannot be read (the file system has none).
func ForeignAccess(path string, dir bool) []string {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nil
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return nil
	}
	if dacl == nil {
		return []string{"Everyone (no DACL)"}
	}
	mask := windows.ACCESS_MASK(fileReadOrWrite)
	if dir {
		mask = dirWrite
	}
	user, _ := currentUserSID()
	var out []string
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(dacl, i, &ace) != nil {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
			ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Mask&mask == 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.String() == user || sid.IsWellKnown(windows.WinLocalSystemSid) ||
			sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			continue
		}
		out = append(out, accountName(sid))
	}
	return out
}

// PrivateFileWarning says, for a secret file, who else can reach it and how
// to fix that; "" when nobody can.
func PrivateFileWarning(path string) string {
	who := ForeignAccess(path, false)
	if len(who) == 0 {
		return ""
	}
	return i18n.Tf("%s is accessible by other users (%s); run: icacls \"%s\" /inheritance:r /grant:r \"%s:F\"",
		"%s доступен другим пользователям (%s); выполните: icacls \"%s\" /inheritance:r /grant:r \"%s:F\"",
		path, strings.Join(who, ", "), path, currentUserName())
}

// PrivateDirWarning says, for a directory holding secrets, who else can
// replace files in it; "" when nobody can.
func PrivateDirWarning(dir string) string {
	who := ForeignAccess(dir, true)
	if len(who) == 0 {
		return ""
	}
	return i18n.Tf("%s is writable by other users (%s): they can replace the files in it; run: icacls \"%s\" /inheritance:r /grant:r \"%s:(OI)(CI)F\"",
		"в %s могут писать другие пользователи (%s) и подменить файлы; выполните: icacls \"%s\" /inheritance:r /grant:r \"%s:(OI)(CI)F\"",
		dir, strings.Join(who, ", "), dir, currentUserName())
}
