// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package config

import "syscall"

// oNoFollow makes the open of a symlink fail instead of following it.
const oNoFollow = syscall.O_NOFOLLOW
