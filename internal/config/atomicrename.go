// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"runtime"
)

// renameRetries is how many times renameAtomic yields and tries again when
// Windows refuses a rename because another handle holds the file open.
const renameRetries = 128

// renameAtomic renames oldpath to newpath. On Windows a file opened for
// reading without share-delete blocks a concurrent rename over it with
// ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION: two calls of one process
// may write and read the same record of an intent at once (D92) — the
// writer yields to the brief read and tries again, bounded. It does not
// wait on the cloud and sleeps on no clock (runtime.Gosched), so it is no
// wait of the machine. On every other OS, and for any other error, the
// first rename is the answer.
func renameAtomic(oldpath, newpath string) error {
	err := os.Rename(oldpath, newpath)
	for i := 0; err != nil && i < renameRetries && renameBlockedByReader(err); i++ {
		runtime.Gosched()
		err = os.Rename(oldpath, newpath)
	}
	return err
}
