// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !unix && !windows

package cli

import "os"

// linkCount: these systems do not report hard links; 0 means unknown.
func linkCount(*os.File, os.FileInfo) uint64 { return 0 }
