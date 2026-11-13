// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package config

// Only Windows blocks a rename over a file open for reading; elsewhere a
// rename is not refused for a concurrent reader.
func renameBlockedByReader(error) bool { return false }
