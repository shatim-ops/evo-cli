// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package journal

// oNoFollow: there is no such flag here; openNoFollow checks the file it
// opened against Lstat of the path instead.
const oNoFollow = 0
