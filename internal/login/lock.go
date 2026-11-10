// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// loginLock is the one evo login of a configuration directory: while one waits for the
// person, another would open a second page on another port, and the person
// could finish the one they did not mean (R-47; plan 1.8, #1087). It is an
// OS lock of the file: it goes away with the process, however it ends, so
// no stale lock is left to clean. The file holds the pid and the port of
// the holder, for the refusal of the next one.
type loginLock struct{ f *os.File }

// lockLogin takes the lock at path, or refuses with STATE_LOCKED naming
// the port of the login that holds it («Риск», conclusion 35.1).
func lockLogin(path string) (*loginLock, error) {
	if err := config.MkdirPrivate(filepath.Dir(path)); err != nil {
		return nil, at(stepLock, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, at(stepLock, err)
	}
	if err := config.LockFile(f); err != nil {
		held, _ := os.ReadFile(path)
		_ = f.Close()
		if errors.Is(err, config.ErrLocked) {
			return nil, at(stepLock, clierrors.New(clierrors.CodeStateLocked, eLoginRunning(filepath.Dir(path), portOf(string(held))), eLoginRunningHint()))
		}
		return nil, at(stepLock, err)
	}
	return &loginLock{f: f}, nil
}

// note writes the holder's pid and port into the file.
func (l *loginLock) note(port int) {
	_ = l.f.Truncate(0)
	_, _ = l.f.WriteAt([]byte(fmt.Sprintf("%d %d\n", os.Getpid(), port)), 0)
}

// release empties the file and keeps it: deleting a file locked by the OS
// races — a process waiting on the unlinked file and the next one creating
// a new file would both hold «the» lock (findings ledger 1.8, № 138).
func (l *loginLock) release() {
	_ = l.f.Truncate(0)
	_ = config.UnlockFile(l.f)
	_ = l.f.Close()
}

// portOf is the port of «pid port», 0 when unknown.
func portOf(held string) int {
	f := strings.Fields(held)
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}
