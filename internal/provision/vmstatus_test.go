// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import "testing"

func TestVMFailed(t *testing.T) {
	for s, want := range map[string]bool{
		"error": true, "error_creating": true, "ERROR_CREATING": true,
		"creating": false, "running": false, "active": false, "": false, "deleting": false,
	} {
		if got := VMFailed(s); got != want {
			t.Errorf("VMFailed(%q) = %v, want %v", s, got, want)
		}
	}
}
