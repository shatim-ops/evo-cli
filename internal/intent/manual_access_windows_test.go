// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The second token has the same user SID, but Windows also requires a grant
// to its restricting SID. Only the disposable sibling gets that grant.
func TestManualWindowsStagedLinkSecondToken(t *testing.T) {
	_, rec, load := manualFixture(t)
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := s.Eligible(rec.Key, "project"); err != nil {
		t.Fatal(err)
	}
	pin, err := s.PrepareTerminal()
	if err != nil {
		t.Fatal(err)
	}

	want, err := os.ReadFile(recordPath(rec.Dir, rec.ID))
	if err != nil || len(want) == 0 {
		t.Fatal("source receipt", err)
	}
	checkOwner := func() {
		t.Helper()
		if b, err := os.ReadFile(pin); err != nil || !bytes.Equal(b, want) {
			t.Fatal("owner could not read the staged receipt", err)
		}
	}
	checkOwner()
	sibling := filepath.Join(filepath.Dir(pin), "access-control.txt")
	const marker = "disposable access control"
	if err := os.WriteFile(sibling, []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid, err := windows.CreateWellKnownSid(windows.WinRestrictedCodeSid)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;" + sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(sibling, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}

	token := manualRestrictedToken(t, sid)
	defer token.Close()
	// The testing goroutine exits on Fatal. If reverting fails, the runtime
	// retires its locked OS thread instead of reusing an impersonated thread.
	runtime.LockOSThread()
	if err := windows.SetThreadToken(nil, token); err != nil {
		runtime.UnlockOSThread()
		t.Fatal(err)
	}
	control, controlErr := os.ReadFile(sibling)
	_, pinErr := os.ReadFile(pin)
	if err := windows.RevertToSelf(); err != nil {
		t.Fatal("revert impersonation", err)
	}
	runtime.UnlockOSThread()
	if controlErr != nil || string(control) != marker {
		t.Fatal("second token could not read the positive control", controlErr)
	}
	if !errors.Is(pinErr, windows.ERROR_ACCESS_DENIED) {
		t.Fatal("second token opening the staged receipt", pinErr)
	}
	checkOwner()
}

func manualRestrictedToken(t *testing.T, sid *windows.SID) windows.Token {
	t.Helper()
	var source windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY, &source); err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	restriction := windows.SIDAndAttributes{Sid: sid}
	var restricted windows.Token
	create := windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")
	const disableMaxPrivilege = 1
	ok, _, err := create.Call(uintptr(source), disableMaxPrivilege, 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&restriction)), uintptr(unsafe.Pointer(&restricted)))
	runtime.KeepAlive(sid)
	if ok == 0 {
		t.Fatal("CreateRestrictedToken", err)
	}
	defer restricted.Close()
	var impersonation windows.Token
	if err := windows.DuplicateTokenEx(restricted, windows.TOKEN_IMPERSONATE|windows.TOKEN_QUERY, nil, windows.SecurityImpersonation, windows.TokenImpersonation, &impersonation); err != nil {
		t.Fatal(err)
	}
	return impersonation
}
