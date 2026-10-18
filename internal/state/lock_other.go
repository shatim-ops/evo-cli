// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !(linux || darwin || freebsd || openbsd || netbsd || dragonfly || windows)

package state

import "os"

// No file locking on this platform: Lock always succeeds.
func lockFile(*os.File) error   { return nil }
func unlockFile(*os.File) error { return nil }
