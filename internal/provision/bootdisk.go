// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// DefaultBootDiskGB is the boot disk size used when none is configured.
const DefaultBootDiskGB = 10

// CheckBootDiskGB refuses a negative boot disk size with USAGE: --disk-size
// -5 silently became 10 GB (QA-14). 0 means unset, the default.
func CheckBootDiskGB(gb int) error {
	if gb >= 0 {
		return nil
	}
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("boot disk size must be a positive number of GB, not %d", "размер загрузочного диска — положительное число ГБ, а не %d", gb),
		i18n.Tf("Pass the size in GB, or leave it out for the default %d GB.", "Укажите размер в ГБ или не задавайте его: по умолчанию %d ГБ.", DefaultBootDiskGB))
}

// BootDiskGB returns the boot disk size to request: the configured size
// (DefaultBootDiskGB when unset), raised to the image's min_disk. The
// create API rejects a smaller disk with 422 vm_root_disk_too_small, and
// min_disk changes when the platform rebuilds an image (ubuntu-22.04 went
// from 10 to 11 GB). The second value explains a raise; it is empty when
// the size is unchanged. A negative size is refused by CheckBootDiskGB
// where it is read (flags, .evo.yaml, deploy_plan); here it is unset.
func BootDiskGB(requested, minDisk int) (int, string) {
	size := requested
	if size <= 0 {
		size = DefaultBootDiskGB
	}
	if minDisk > size {
		note := i18n.Tf("boot disk raised from %d to %d GB: the image requires at least %d GB",
			"загрузочный диск увеличен с %d до %d ГБ: образу нужно не меньше %d ГБ", size, minDisk, minDisk)
		return minDisk, note
	}
	return size, ""
}
