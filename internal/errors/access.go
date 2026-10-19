// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors

import "github.com/shatim-ops/evo-cli/internal/i18n"

// The hints of a Cloud.ru service that refused a request IAM gave a token
// for. 401 and 403 mean different things to fix: the key (revoked, expired,
// the account disabled) or the role of the service account in the project.
// Compute reports them as AUTH_EXPIRED / AUTH_FAILED; the other services as
// API_ERROR (docs/exit-codes.md), and before these hints said neither.

// TokenRejectedHint is the hint of a 401 from a service.
func TokenRejectedHint() string {
	return i18n.T("The key may have been deleted or expired, or the service account disabled. Check the access key with 'evo doctor', or run 'evo configure' again.",
		"Ключ мог быть удалён или истечь, а сервисный аккаунт — выключен. Проверьте ключ доступа командой 'evo doctor' или заново выполните 'evo configure'.")
}

// NoRoleHint is the hint of a 403 from a service: the key is valid, the
// service account lacks a role.
func NoRoleHint() string {
	return i18n.T("The key is valid, but the service account lacks a role for this operation in the project (or project_id is another project). "+
		"Grant it in the Cloud.ru console: Users → Service accounts → the account; which roles: docs/auth.md, «Роли».",
		"Ключ принят, но у сервисного аккаунта нет роли для этой операции в проекте (или project_id — чужой проект). "+
			"Выдайте роль в консоли Cloud.ru: «Пользователи» → «Сервисные аккаунты» → аккаунт; какие роли нужны — docs/auth.md, раздел «Роли».")
}
