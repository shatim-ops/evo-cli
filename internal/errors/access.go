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

// TokenRejectedHint is the hint of a 401 from a service. evo has dropped
// the rejected token by then (httpretry.OnTokenRejected): a token that
// expired is replaced on the next call, and only a 401 that comes again
// points at the key.
func TokenRejectedHint() string {
	return i18n.T("The token may have expired: evo has dropped it and gets a new one on the next call — repeat the command. If the 401 comes again, the key may have been deleted or expired, or the service account disabled: check the access key with 'evo doctor', or run 'evo configure' again.",
		"Токен мог истечь: evo сбросил его и получит новый при следующем вызове — повторите команду. Если 401 повторяется, ключ мог быть удалён или истечь, а сервисный аккаунт — выключен: проверьте ключ доступа командой 'evo doctor' или заново выполните 'evo configure'.")
}

// NoRoleHint is the hint of a 403 from a service: the key is valid, the
// service account lacks a role.
func NoRoleHint() string {
	return i18n.T("The key is valid, but the service account lacks a role for this operation in the project (or project_id is another project). "+
		"Grant it in the Cloud.ru console: Users → Service accounts → the account; which roles: docs/auth.md, «Роли».",
		"Ключ принят, но у сервисного аккаунта нет роли для этой операции в проекте (или project_id — чужой проект). "+
			"Выдайте роль в консоли Cloud.ru: «Пользователи» → «Сервисные аккаунты» → аккаунт; какие роли нужны — docs/auth.md, раздел «Роли».")
}

// S3AccessHint is the hint of a 403 from Object Storage: S3 is reached
// with keys of its own, not the role of the service account (Ц-3 of the
// agent's audit of rc.1: the cause was the tenant_id of the access key).
func S3AccessHint() string {
	return i18n.T("Object Storage refused the request: it is reached with the S3 keys, not the key of evo. Check CLOUDRU_S3_ACCESS_KEY (or access_key of the profile's s3 section) — <tenant_id>:<Key ID>, the tenant_id from the console: Object Storage → Parameters — and CLOUDRU_S3_SECRET_KEY, then that the service account of the S3 key has an Object Storage role (s3e.*); docs/auth.md, «Ключи Object Storage (S3)».",
		"Object Storage отказал: он принимает ключи S3, а не ключ evo. Проверьте CLOUDRU_S3_ACCESS_KEY (или access_key в разделе s3 профиля) — <tenant_id>:<Key ID>, tenant_id — в консоли: «Object Storage» → «Параметры» — и CLOUDRU_S3_SECRET_KEY, затем что у сервисного аккаунта ключа S3 есть роль Object Storage (s3e.*); docs/auth.md, раздел «Ключи Object Storage (S3)».")
}
