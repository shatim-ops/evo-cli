// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func TestImageDeleteConnectionRefused(t *testing.T) {
	checkDeleteConnectionRefused(t, "image", "delete", "image_delete", "image_id", "images", parityVM)
}
