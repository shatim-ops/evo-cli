// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"errors"
	"os"
	"syscall"
)

// processAlive tells whether the process pid may be running: only a
// process that cannot be found, or one that is known to be done, is not.
// Any other answer — no right to ask, a system that does not tell — is a
// process that lives: in doubt a record is not another call's to remove.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	defer p.Release()
	err = p.Signal(syscall.Signal(0))
	return !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH)
}
