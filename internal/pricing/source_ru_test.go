// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pricing

import "testing"

// Every source this package makes has a Russian text for the CLI, and an
// unknown one has none (D6; Астра #1835): the value itself is not changed.
func TestSourceRU_EverySourceOfThePackage(t *testing.T) {
	for _, s := range []string{SourceAPI, SourceAPIDisk, SourceDiskCard, SourceVCPURAM, SourceVCPUShare, SourceFlavorCard, EstimatePG(PGSize{CPU: 2, RAMGB: 4, StorageGB: 10, Instances: 1}).Source} {
		if ru, ok := SourceRU(s); !ok || ru == "" || ru == s {
			t.Errorf("SourceRU(%q) = %q, %v", s, ru, ok)
		}
	}
	if _, ok := SourceRU("something else"); ok {
		t.Error("an unknown source has a Russian text")
	}
}
