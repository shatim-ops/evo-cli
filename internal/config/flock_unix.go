// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package config

import (
	"errors"
	"os"
	"syscall"
)

// flock locks belong to the open file, so a second open in the same
// process (MCP over HTTP serves calls concurrently) is refused too.
//
// LockFile takes an exclusive OS lock of f without waiting: ErrLocked when
// another holder has it. The lock goes away with the process.
func LockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrLocked
	}
	return err
}

// UnlockFile releases LockFile's lock.
func UnlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
