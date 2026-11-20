// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func TestDiskDeleteConnectionRefused(t *testing.T) {
	checkDeleteConnectionRefused(t, "disk", "delete", "disk_delete", "disk_id", "disks", parityVM)
}
