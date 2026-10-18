// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !(linux || darwin || freebsd || openbsd || netbsd || dragonfly || windows)

package config

import "os"

// No file locking on this platform: LockFile always succeeds.
func LockFile(*os.File) error   { return nil }
func UnlockFile(*os.File) error { return nil }
