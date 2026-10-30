// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors

import "github.com/shatim-ops/evo-cli/internal/i18n"

// ProjectRequired is the error of a command or tool whose API call needs
// the project and none is set. Nothing is sent: the API would answer 422
// "query.project_id: Field required", which read as a Cloud.ru failure
// (API_ERROR, exit 5) instead of a setting to fix (release audit, R-06).
func ProjectRequired() *CLIError {
	return New(CodeConfigInvalid,
		i18n.T("no project is set (project_id)", "не задан проект (project_id)"),
		i18n.T("Set it: 'evo project use <UUID|name>' (kept in the profile), CLOUDRU_PROJECT_ID, or the project_id of the call. The project UUID is in the Cloud.ru console.",
			"Задайте проект: 'evo project use <UUID|имя>' (сохранится в профиле), CLOUDRU_PROJECT_ID или project_id вызова. UUID проекта — в консоли Cloud.ru."))
}
