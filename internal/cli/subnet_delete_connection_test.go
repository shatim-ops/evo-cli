// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func TestSubnetDeleteConnectionRefused(t *testing.T) {
	checkDeleteConnectionRefused(t, "vpc", "subnet-delete", "subnet_delete", "subnet_id", "subnets", parityVM)
}
