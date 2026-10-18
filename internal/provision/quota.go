// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import "strings"

// IsQuotaError reports whether an error looks like a Cloud.ru quota rejection.
func IsQuotaError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "quota") || strings.Contains(s, "organization_quota_exceeded")
}
