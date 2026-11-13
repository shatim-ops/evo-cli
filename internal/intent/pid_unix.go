// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package intent

import (
	"errors"
	"syscall"
)

// processAlive tells whether the process pid may be running: only «no such
// process» says it is not — a process of another user answers EPERM, and
// any other doubt is a process that lives.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
