// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package config

import "os"

// MkdirPrivate creates dir and its missing parents with mode 0700. An
// existing directory is left as it is.
func MkdirPrivate(dir string) error { return os.MkdirAll(dir, 0o700) }

// createPrivateTemp is os.CreateTemp: the file is created 0600.
func createPrivateTemp(dir, pattern string) (*os.File, error) { return os.CreateTemp(dir, pattern) }

// RestrictToOwner is a no-op: Unix callers chmod themselves. It exists for
// the Windows ACL counterpart.
func RestrictToOwner(string, bool) error { return nil }

// ForeignAccess is nil: on Unix the file mode answers this (LooseMode).
func ForeignAccess(string, bool) []string { return nil }

// PrivateFileWarning is "": the Unix checks look at the file mode.
func PrivateFileWarning(string) string { return "" }

// PrivateDirWarning is "": the Unix checks look at the directory mode.
func PrivateDirWarning(string) string { return "" }
