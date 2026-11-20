// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func TestFipDeleteConnectionRefused(t *testing.T) {
	checkDeleteConnectionRefused(t, "vpc", "fip-release", "release_floating_ip", "id", "floating-ips", parityVM)
}
