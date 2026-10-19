// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// QA-14: a negative size is USAGE, not the default; 0 is unset.
func TestCheckBootDiskGB(t *testing.T) {
	for gb, wantErr := range map[int]bool{-5: true, -1: true, 0: false, 10: false, 500: false} {
		err := CheckBootDiskGB(gb)
		if (err != nil) != wantErr || (err != nil && (cliCode(err) != clierrors.CodeUsage || clierrors.ExitCode(err) != clierrors.ExitGeneral)) {
			t.Errorf("CheckBootDiskGB(%d) = %v", gb, err)
		}
	}
}

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
