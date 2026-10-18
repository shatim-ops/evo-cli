// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import "testing"

func TestBootDiskGB(t *testing.T) {
	cases := []struct {
		requested, minDisk, want int
		raised                   bool
	}{
		{0, 0, DefaultBootDiskGB, false},
		{0, 11, 11, true}, // ubuntu-22.04 since 15.10.2026: default 10 → 422
		{20, 11, 20, false},
		{8, 11, 11, true}, // an explicit size below the minimum fails anyway
		{11, 11, 11, false},
		{-1, 5, DefaultBootDiskGB, false},
	}
	for _, c := range cases {
		got, note := BootDiskGB(c.requested, c.minDisk)
		if got != c.want || (note != "") != c.raised {
			t.Errorf("BootDiskGB(%d, %d) = %d, %q; want %d, raised=%v", c.requested, c.minDisk, got, note, c.want, c.raised)
		}
	}
}
