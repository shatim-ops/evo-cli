// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/product"
)

// Every text of evo login and logout is here. The Russian texts are the
// final set of «Тексты входа — редактура» (docs/internal/briefs/
// login-texts.md, #1022), with the conditions of «Риск», conclusion 28; a
// new edition is a change of these strings, not of the code (#986). The
// page names the actions and «Evolution CMCP», the terminal the commands
// and evo (D34, variant A). No «ё» (the owner's decision 05.11).

// day is a date as the texts write it: 05.12.2026.
func day(t time.Time) string { return t.Local().Format("02.01.2006") }

// 1. The consent page.

func tHeading() string {
	return i18n.Tf("%s requests access to a project", "%s запрашивает доступ к проекту", product.PageName)
}
func tSignedIn(login string) string {
	return i18n.Tf("You are signed in as %s. %s will create a service account (or reuse the existing one) and an access key to manage the cloud in the project you choose",
		"Вы вошли как %s. %s создаст сервисный аккаунт (или использует уже созданный) и ключ доступа, чтобы управлять облаком в выбранном проекте",
		login, product.PageName)
}
func tProject() string { return i18n.T("Project", "Проект") }
func tEnvProjectPicked() string {
	return i18n.T("Selected: the project from CLOUDRU_PROJECT_ID", "Выбран проект из CLOUDRU_PROJECT_ID")
}
func tEnvProjectNoRight() string {
	return i18n.T("The project from CLOUDRU_PROJECT_ID is not selected: project administrator rights are required",
		"Проект из CLOUDRU_PROJECT_ID не выбран: нужны права администратора проекта")
}

// tProfileProjectNoRight: the same for the project of the profile, which
// the page checks without a caption (conclusion 27) but explains when it
// cannot (the owner's decision, #1000).
func tProfileProjectNoRight() string {
	return i18n.T("The project of the current evo profile is not selected: project administrator rights are required",
		"Проект текущего профиля evo не выбран: нужны права администратора проекта")
}
func tNoRightMark() string {
	return i18n.T("project administrator rights required", "нужны права администратора проекта")
}

// tNoRightNote and eNoRightHint name the way of a person without the right
// («Риск», conclusion 28.2): the administrator makes the account and key.
func tNoRightNote() string {
	return i18n.T("If you are not a project administrator, ask the project administrator to create a service account with the required role and a key for you in the Cloud.ru personal account",
		"Если прав администратора в проекте нет, попросите администратора проекта создать для вас сервисный аккаунт с нужной ролью и ключ в личном кабинете Cloud.ru")
}
func tForWhom() string { return i18n.T("Who the key is for", "Кому ключ") }
func tForMe() string   { return i18n.T("For me", "Мне") }
func tForMeNote() string {
	return i18n.T("Account evo: you run the commands", "Аккаунт evo: команды запускаете вы")
}
func tForAgent() string { return i18n.T("For an agent", "Агенту") }
func tForAgentNote() string {
	return i18n.T("Account evo-agent: the agent works on its own through the MCP server", "Аккаунт evo-agent: агент работает сам через MCP-сервер")
}

// tRoleNote says what the role allows; «any resources of the project»
// stays the agent's warning, once on the page (conclusion 26.3).
func tRoleNote() string {
	return i18n.Tf("Both accounts get the role “%s”. It allows deleting project resources and managing service accounts and access keys",
		"Оба аккаунта получают роль «%s». Она дает право удалять ресурсы проекта и управлять сервисными аккаунтами и ключами доступа",
		auth.LoginRoleDisplayName)
}
func tAgentWarnTitle() string {
	return i18n.T("The agent will get project administrator rights", "Агент получит права администратора проекта")
}
func tAgentWarn(until string) string {
	return i18n.Tf("The role “%s” gives the agent full access to the project: it can create, modify and delete any resources of the project, including those not created through %s, and manage service accounts and keys. The role is granted until %s. To revoke access earlier, sign out of %s on this computer — the key will be deleted. The role is removed in the Cloud.ru personal account",
		"Роль «%s» дает агенту полный доступ к ресурсам проекта: он может создавать, изменять и удалять любые ресурсы проекта, в том числе созданные не через %s, а также управлять сервисными аккаунтами и ключами. Роль выдается со сроком до %s. Чтобы отозвать доступ раньше, выйдите из %s на этом компьютере — ключ будет удален. Роль снимается в личном кабинете Cloud.ru",
		auth.LoginRoleDisplayName, product.PageName, until, product.PageName)
}
func tTerm() string      { return i18n.T("Key validity", "Срок ключа") }
func tDays(n int) string { return i18n.Tf("%d days", "%d дней", n) }
func tForeverInConsole() string {
	return i18n.T("Keys that never expire can be created only in the Cloud.ru personal account", "Бессрочные ключи создаются только в личном кабинете Cloud.ru")
}

// tUnchecked is the one mark of the right on the page and in the terminal
// (conclusions 26.5 and 28.1): what evaluate answered, by the list.
func tUnchecked(reason string) string {
	return i18n.Tf("The platform has not confirmed the right: %s. If the right is missing, Cloud.ru will refuse to create the account, and nothing will be left behind",
		"Право не подтверждено платформой: %s. Если права нет, Cloud.ru откажет при создании аккаунта, и ничего не останется", reason)
}

// The reasons of tUnchecked: what evaluate answered («Риск», R-49).
func tEvaluateFailed(err string) string {
	return i18n.Tf("evaluate returned %s", "evaluate ответил %s", err)
}
func tActionUnknown(action string) string {
	return i18n.Tf("evaluate returned false for %s; the action is not in the IAM catalog", "evaluate ответил false на %s; действия нет в каталоге IAM", action)
}
func tCatalogUnread(action, err string) string {
	return i18n.Tf("evaluate returned false for %s; the IAM action catalog could not be read: %s",
		"evaluate ответил false на %s; каталог действий IAM не прочитан: %s", action, err)
}
func tWhatWillBeMade() string { return i18n.T("What will be done", "Что будет сделано") }
func tSumAccountNew(account, project string) string {
	return i18n.Tf("The service account %s will be created in the project %s (or the existing one reused).",
		"Сервисный аккаунт %s в проекте %s будет создан (или использован уже созданный).", account, project)
}
func tSumRole(until string) string {
	return i18n.Tf("The role “%s” will be granted until %s.", "Роль «%s» будет выдана до %s.", auth.LoginRoleDisplayName, until)
}
func tSumKey(until string) string {
	return i18n.Tf("The access key will be valid until %s.", "Ключ доступа будет действовать до %s.", until)
}

// tSumStaysPage and tSumStaysTerm: whether the account is new is known
// only after the page (step 12), so the role «expires in its term», not
// «with the key» («Риск», conclusion 28.3).
func tSumStaysPage() string {
	return i18n.Tf("After you sign out of %s, the account remains and the role expires at the end of its term",
		"После выхода из %s аккаунт останется, а роль истечет в свой срок", product.PageName)
}
func tSumStaysTerm() string {
	return i18n.T("After you sign out (evo logout), the account remains and the role expires at the end of its term",
		"После выхода (evo logout) аккаунт останется, а роль истечет в свой срок")
}

// tGrantInProject is the main button; the page puts the chosen project's
// name where %s is, by CSS.
func tGrantInProject(project string) string {
	return i18n.Tf("Grant access to the project “%s”", "Выдать доступ к проекту «%s»", project)
}
func tCancel() string { return i18n.T("Cancel", "Отмена") }
func tSecretNote() string {
	return i18n.Tf("The key's secret never passes through the browser and is never shown: %s on this computer creates the account and the key with your rights and saves the key to the profile",
		"Секрет ключа не проходит через браузер и нигде не показывается: аккаунт и ключ создает %s на этом компьютере с вашими правами и записывает ключ в профиль",
		product.PageName)
}

// 2. «Done»: the dates are the role's actual term (conclusion 26.2).

func tDone() string { return i18n.T("Access granted", "Доступ выдан") }

// tDoneText is the result by whom the key is for.
func tDoneText(keyID, profile string, term time.Time, forAgent bool) string {
	if forAgent {
		return i18n.Tf("The key %s is saved on this computer in the profile “%s” and is valid until %s. The agent will continue on its own — go back to it",
			"Ключ %s сохранен на этом компьютере в профиле «%s» и действует до %s. Агент продолжит работу сам — вернитесь к нему", keyID, profile, day(term))
	}
	return i18n.Tf("The key %s is saved on this computer in the profile “%s” and is valid until %s. You can go back to work and close this window",
		"Ключ %s сохранен на этом компьютере в профиле «%s» и действует до %s. Можно вернуться к работе — это окно можно закрыть", keyID, profile, day(term))
}

// tDoneReused: only when the account was there; without the last part
// when there was no previous key.
func tDoneReused(account string, role time.Time, previousKey string) string {
	if previousKey == "" {
		return i18n.Tf("The existing service account %s was reused: the role “%s” is valid until %s",
			"Использован существующий сервисный аккаунт %s: роль «%s» действует до %s", account, auth.LoginRoleDisplayName, day(role))
	}
	return i18n.Tf("The existing service account %s was reused: the role “%s” is valid until %s; the previous key %s on this computer was deleted",
		"Использован существующий сервисный аккаунт %s: роль «%s» действует до %s, прежний ключ %s на этом компьютере удален",
		account, auth.LoginRoleDisplayName, day(role), previousKey)
}

// The row names of the result page; the terminal's rows are tRow*.
func tRowsPage() [5]string {
	return [5]string{i18n.T("Project", "Проект"), i18n.T("Account", "Аккаунт"), i18n.T("Role", "Роль"),
		i18n.T("Key", "Ключ"), i18n.T("Profile", "Профиль")}
}
func tRoleUntil(term time.Time) string {
	return i18n.Tf("%s, until %s", "%s, до %s", auth.LoginRoleDisplayName, day(term))
}
func tRevokeHow() string {
	return i18n.Tf("To revoke access, sign out of %s or delete the key in the Cloud.ru personal account under “Service accounts”. You can close this window",
		"Чтобы закрыть доступ, выйдите из %s или удалите ключ в личном кабинете Cloud.ru в разделе «Сервисные аккаунты». Это окно можно закрыть",
		product.PageName)
}

// 3. «Cancelled» and the error page: the page shows the message, the hint
// with its command stays the terminal's and JSON's.

func tCancelledTitle() string { return i18n.T("Cancelled", "Отменено") }
func tCancelled() string {
	return i18n.T("Nothing was created. To start over, sign in again. You can close this window",
		"Ничего не создано. Чтобы начать заново, запустите вход еще раз. Это окно можно закрыть")
}
func tNotComplete() string {
	return i18n.T("Access could not be granted", "Не удалось выдать доступ")
}
func tDetails(reason string, code int) string {
	return i18n.Tf("See the details (%s, code %d) where you started the sign-in. To try again, sign in again. You can close this window",
		"Подробности (%s, код %d) — там, где запускали вход. Чтобы повторить, запустите вход еще раз. Это окно можно закрыть", reason, code)
}
func tForeign() string {
	return i18n.T("This request does not belong to the current sign-in. Sign in again", "Запрос не относится к текущему входу. Запустите вход еще раз")
}

// 4. The terminal.

func tChoose() string {
	return i18n.T("Choose an option (number)", "Выберите вариант (номер)")
}

// tAgentWarnLine and tAgentRevokeLine: the warning of a key for an agent in
// the terminal, once a login; the exit is the command there (conclusion
// 27). «Any resources of the project» once (conclusion 26.3).
func tAgentWarnLine(until string) string {
	if until == "" {
		return i18n.T("The agent will get project administrator rights: it can delete any resources of the project, including those not created through evo CMCP, and issue new keys",
			"Агент получит права администратора проекта: любые ресурсы проекта, в том числе созданные не через evo CMCP, он может удалять, а также выпускать новые ключи")
	}
	return i18n.Tf("The agent will get project administrator rights: it can delete any resources of the project, including those not created through evo CMCP, and issue new keys. The role is granted until %s",
		"Агент получит права администратора проекта: любые ресурсы проекта, в том числе созданные не через evo CMCP, он может удалять, а также выпускать новые ключи. Роль выдается со сроком до %s", until)
}
func tAgentRevokeLine() string {
	return i18n.T("To revoke earlier: evo logout deletes the key; the role is removed in the Cloud.ru personal account",
		"Отозвать раньше: evo logout удалит ключ, роль снимается в личном кабинете Cloud.ru")
}
func tTermQuestion() string {
	return i18n.T("Key validity, days (7, 30, 90)", "Срок ключа, дней (7, 30, 90)")
}
func tWhatWillBeDone() string {
	return i18n.T("What will be done:", "Что будет сделано:")
}
func tRowAccount() string { return i18n.T("account", "аккаунт") }
func tRowRole() string    { return i18n.T("role", "роль") }
func tRowKey() string     { return i18n.T("key", "ключ") }
func tRowProfile() string { return i18n.T("profile", "профиль") }
func tKeyDays(days int, term time.Time) string {
	return i18n.Tf("%d days, until %s", "%d дней, до %s", days, day(term))
}
func tGrantOnPage() string {
	return i18n.T("You choose the project on the page that opens after sign-in. Access is granted only by the “Grant access” button on that page.",
		"Проект выбирается на странице, которая откроется после входа. Доступ выдается только кнопкой «Выдать доступ» на этой странице.")
}

// tOpenPage is said once before the browser (D34, variant A).
func tOpenPage() string {
	return i18n.Tf("The %s page will open", "Откроется страница %s", product.PageName)
}
func tOpen() string {
	return i18n.T("Signing in to Cloud.ru in the browser. If the browser did not open, follow the link:",
		"Вход в Cloud.ru через браузер. Если браузер не открылся, перейдите по ссылке:")
}
func tWaiting(wait time.Duration) string {
	return i18n.Tf("Finish signing in in the browser. Waiting up to %d min; press Ctrl-C to cancel",
		"Завершите вход в браузере. Ожидание — до %d мин, отмена — Ctrl-C", int(wait.Minutes()))
}
func tNoBrowser() string {
	return i18n.T("The browser was not opened: open the link on this same computer. Sign-in does not work over SSH or in a container: after sign-in, the browser returns to 127.0.0.1 on its own computer, not on the one evo runs on",
		"Браузер не открыт: откройте ссылку на этом же компьютере. По SSH и в контейнере вход не пройдет: после входа браузер возвращается на 127.0.0.1 своего компьютера, а не того, где запущен evo")
}
func tStepAccount(account string, reused bool) string {
	if reused {
		return i18n.Tf("account %s… already exists", "аккаунт %s… уже есть", account)
	}
	return i18n.Tf("account %s… created", "аккаунт %s… создан", account)
}
func tStepRole() string { return i18n.T("role… granted", "роль… выдана") }
func tStepKey() string  { return i18n.T("key… issued", "ключ… выпущен") }
func tStepKeyCheck() string {
	return i18n.T("key check… passed", "проверка ключа… пройдена")
}

// tPreselectTerminal names the preselected project and its source in the
// terminal, always (conclusion 27).
func tPreselectTerminal(project, source string) string {
	return i18n.Tf("The page has the project “%s” selected, %s", "На странице отмечен проект «%s», %s", project, source)
}

// TFromProfile and TFromEnv name where a preselected project came from.
func TFromProfile(profile string) string {
	return i18n.Tf("from the evo profile “%s”", "из профиля evo «%s»", profile)
}
func TFromEnv() string { return i18n.T("from CLOUDRU_PROJECT_ID", "из CLOUDRU_PROJECT_ID") }

// The result in the terminal (internal/cli): the dates are the role's
// actual term (conclusion 26.2).
func TSignedInCLI(keyID, account, project, profile string, until time.Time) string {
	return i18n.Tf("Access granted: the key %s of %s in the project %s is saved to the profile “%s”, valid until %s",
		"Доступ выдан: ключ %s аккаунта %s в проекте %s записан в профиль «%s», действует до %s",
		keyID, account, project, profile, day(until))
}
func TResultRole(until time.Time) string {
	return i18n.Tf("The role “%s” is valid until %s", "Роль «%s» действует до %s", auth.LoginRoleDisplayName, day(until))
}
func TResultReused(account, previousKey string) string {
	if previousKey == "" {
		return i18n.Tf("The existing service account %s was reused", "Использован существующий сервисный аккаунт %s", account)
	}
	return i18n.Tf("The existing service account %s was reused; the previous key %s was deleted",
		"Использован существующий сервисный аккаунт %s, прежний ключ %s удален", account, previousKey)
}
func TResultCheck(profile string) string {
	return i18n.Tf("Check: evo whoami --profile %s", "Проверка: evo whoami --profile %s", profile)
}

// 5. The errors: the message and the hint of the terminal and JSON.

func eNoYes() string {
	return i18n.T("evo login has no --yes flag: access is granted only by the “Grant access” button on the page",
		"у evo login нет флага --yes: доступ выдается только кнопкой «Выдать доступ» на странице")
}
func eNoYesHint() string {
	return i18n.T("Run evo login without --yes. A person grants access with the button on the page. Retrying with --yes will not help.",
		"Запустите evo login без --yes. Доступ выдает человек кнопкой на странице. Повтор с --yes не поможет.")
}

// RefuseYes is the refusal of evo login --yes, before any request: the
// consent is the page's («Риск», conclusion 24).
func RefuseYes() error { return clierrors.New(clierrors.CodeUsage, eNoYes(), eNoYesHint()) }

func eCancelled() string {
	return i18n.T("the sign-in was cancelled, nothing was created", "вход отменен, ничего не создано")
}
func eCancelledHint() string {
	return i18n.T("To start over, run evo login.", "Чтобы начать заново, выполните evo login.")
}
func eSession(status int) string {
	return i18n.Tf("sign-in failed: the sign-in session expired (HTTP %d)", "вход не выполнен: сессия входа истекла (HTTP %d)", status)
}
func eRetry() string { return i18n.T("Run evo login again.", "Повторите evo login.") }
func eIDToken(what string) string {
	return i18n.Tf("IAM did not confirm the sign-in (ID token: %s); nothing was created", "IAM не подтвердил вход (ID token: %s), ничего не создано", what)
}
func eIDTokenHint() string {
	return i18n.T("Run evo login again. If the error persists, contact Cloud.ru support.",
		"Повторите evo login. Если ошибка повторится, обратитесь в поддержку Cloud.ru.")
}
func eNoRight(project string) string {
	return i18n.Tf("not enough rights to grant access in the project “%s”", "недостаточно прав для выдачи доступа в проекте «%s»", project)
}
func eNoRightAny(n int) string {
	return i18n.Tf("not enough rights to grant access in any of your %d projects", "недостаточно прав для выдачи доступа ни в одном из %d проектов", n)
}
func eNoRightHint() string {
	return i18n.T("Ask the project administrator to create a service account with the required role and a key for you in the Cloud.ru personal account. Retrying will not help.",
		"Попросите администратора проекта создать для вас сервисный аккаунт с нужной ролью и ключ в личном кабинете Cloud.ru. Повтор не поможет.")
}

// eForbidden: the platform's 403 at the first write («Риск», R-49).
func eForbidden(project string) string {
	return i18n.Tf("not enough rights to grant access in the project “%s”: Cloud.ru refused", "недостаточно прав для выдачи доступа в проекте «%s»: Cloud.ru отказал", project)
}
func eAccountLimit(project string, n int) string {
	return i18n.Tf("the project “%s” has reached its limit of service accounts (%d); no new account was created",
		"в проекте «%s» достигнут лимит сервисных аккаунтов (%d), новый не создан", project, n)
}
func eAccountLimitHint() string {
	return i18n.T("Delete an unused service account in the Cloud.ru personal account, or choose another project.",
		"Удалите неиспользуемый сервисный аккаунт в личном кабинете Cloud.ru или выберите другой проект.")
}
func eKeyLimit(account string, n int) string {
	return i18n.Tf("the service account %s has reached its limit of keys (%d); no new key was created",
		"у сервисного аккаунта %s достигнут лимит ключей (%d), новый не создан", account, n)
}
func eKeyLimitHint(account string) string {
	return i18n.Tf("Delete unused keys in the Cloud.ru personal account: “Service accounts” → %s.",
		"Удалите неиспользуемые ключи в личном кабинете Cloud.ru: «Сервисные аккаунты» → %s.", account)
}
func eNotComplete(cause string, made, undone, left []string) string {
	return i18n.Tf("access was not granted: %s; created: %v; rolled back: %v; left behind: %v", "доступ не выдан: %s; создано: %v; отменено: %v; осталось: %v",
		cause, made, undone, left)
}
func eNotCompleteHint(left bool) string {
	if left {
		return i18n.T("Delete what was left behind in the Cloud.ru personal account under “Service accounts” and run evo login again.",
			"Удалите оставшееся в личном кабинете Cloud.ru в разделе «Сервисные аккаунты» и повторите evo login.")
	}
	return i18n.T("Nothing was left behind. Run evo login again.", "Ничего не осталось. Повторите evo login.")
}
func ePorts(ports []int) string {
	return i18n.Tf("ports %v are in use; nothing was created", "порты %v заняты, ничего не создано", ports)
}
func ePortsHint() string {
	return i18n.T("Free one of the ports and run evo login again.", "Освободите один из портов и повторите evo login.")
}
func eTimeout(wait time.Duration) string {
	return i18n.Tf("sign-in was not completed within %d min; nothing was created", "вход не выполнен за %d мин, ничего не создано", int(wait.Minutes()))
}
func eProvider(what string) string {
	return i18n.Tf("Cloud.ru rejected the sign-in: %s; nothing was created", "Cloud.ru отклонил вход: %s; ничего не создано", what)
}
func eProviderHint() string {
	return i18n.T("Run evo login again. If the error persists, ask your organization administrator to check the sign-in settings of your account.",
		"Повторите evo login. Если ошибка повторится, обратитесь к администратору организации: он проверит настройки входа вашей учетной записи.")
}
func eKeyCheck(status int) string {
	return i18n.Tf("IAM issued no token for the new key (HTTP %d)", "IAM не выдал токен по новому ключу (HTTP %d)", status)
}
func eChoice(what string) string {
	return i18n.Tf("invalid value (%s); nothing was created", "недопустимое значение (%s), ничего не создано", what)
}
func eNoProject(project, login string) string {
	if project == "" {
		return i18n.Tf("%s has no available projects; nothing was created", "у %s нет доступных проектов, ничего не создано", login)
	}
	return i18n.Tf("the project “%s” was not found among the projects of %s; nothing was created", "проект «%s» не найден среди проектов %s, ничего не создано", project, login)
}
func eNoProjectHint() string {
	return i18n.T("Create a project in the Cloud.ru personal account, or sign in as a project member.",
		"Создайте проект в личном кабинете Cloud.ru или войдите как участник проекта.")
}
func eNoProjectHintFlag() string {
	return i18n.T("Pass in --project the ID or name of a project from the Cloud.ru personal account, or run evo login without --project to choose the project on the page.",
		"Укажите в --project ID или имя проекта из личного кабинета Cloud.ru или запустите evo login без --project, чтобы выбрать проект на странице.")
}
func eKeyLeft(keyID string, err error) string {
	return i18n.Tf("the previous key %s was not deleted: %v. Delete it in the Cloud.ru personal account", "прежний ключ %s не удален: %v. Удалите его в личном кабинете Cloud.ru", keyID, err)
}

// tAt names the step of a refusal and the HTTP status of the answer that
// made it, when one did (#981).
func tAt(step string, status int) string {
	if status != 0 {
		return i18n.Tf(" (step %s, HTTP %d)", " (шаг %s, HTTP %d)", step, status)
	}
	return i18n.Tf(" (step %s)", " (шаг %s)", step)
}
func eOIDCUnreachable() string {
	return i18n.T("the sign-in server of Cloud.ru (OpenID Connect) is unreachable", "сервер входа Cloud.ru (OpenID Connect) недоступен")
}
func eNetworkRetry() string {
	return i18n.T("Check the network connection and run evo login again.", "Проверьте подключение к сети и повторите evo login.")
}
func eDiscovery(issuer, what string) string {
	return i18n.Tf("evo did not accept the OpenID Connect sign-in settings from %s: %s; nothing was sent",
		"evo не принял настройки входа OpenID Connect от %s: %s; ничего не отправлено", issuer, what)
}
func eDiscoveryHint() string {
	return i18n.T("Your account is fine: the IAM response did not match what was expected. Try again later. If the error persists, report it to Cloud.ru support.",
		"С учетной записью все в порядке: ответ IAM не совпал с ожидаемым. Повторите позже. Если ошибка повторится, сообщите о ней в поддержку Cloud.ru.")
}
func eOrgUnreachable() string {
	return i18n.T("the Organization API of Cloud.ru is unreachable", "Organization API Cloud.ru недоступен")
}
func eOrgStatus(status int, path string) string {
	return i18n.Tf("the Organization API returned %d (%s)", "Organization API вернул %d (%s)", status, path)
}
func eOrgPages(path string) string {
	return i18n.Tf("Organization API: %s returns too many pages", "Organization API: у %s слишком много страниц", path)
}

// The items of «created / undone / left».
func tUndoAccount(account string) string {
	return i18n.Tf("service account %s", "сервисный аккаунт %s", account)
}
func tUndoKey() string  { return i18n.T("access key", "ключ доступа") }
func tUndoRole() string { return i18n.T("the account's role", "роль аккаунта") }

// 6. evo agent setup about a key of evo login.
func TAgentKeyWarning(profile string) string {
	return i18n.Tf("the key of the profile “%s” was created by evo login with the role “%s”: with it, the agent can delete any resources of the project. An agent working unattended needs a key whose role cannot delete",
		"ключ профиля «%s» создан через evo login с ролью «%s»: с ним агент может удалять любые ресурсы проекта. Для работы агента без присмотра нужен ключ с ролью без права удаления",
		profile, auth.LoginRoleDisplayName)
}

// 7. evo logout.
func TLogoutShort() string {
	return i18n.T("Delete the access key created by evo login, and the profile", "Удалить ключ доступа, созданный evo login, и профиль")
}
func TLogoutLong() string {
	return i18n.T(`Delete the access key evo login created for the profile, then the profile
itself and its saved token. The service account and its role remain. If the
profile's key was created by evo configure or in the Cloud.ru personal
account, only the profile is deleted and the key remains in the cloud.`, `Удалить ключ доступа, который evo login создал для профиля, затем сам
профиль и сохраненный токен. Сервисный аккаунт и его роль остаются. Если
ключ профиля создан через evo configure или в личном кабинете Cloud.ru,
удаляется только профиль, а ключ остается в облаке.`)
}
func TLogoutRevoke(keyID, profile string) string {
	return i18n.Tf("delete the access key %s and the profile “%s”", "удалить ключ доступа %s и профиль «%s»", keyID, profile)
}
func TLogoutDelete(profile string) string {
	return i18n.Tf("delete the profile “%s”", "удалить профиль «%s»", profile)
}
func ENoProfile(profile string) string {
	return i18n.Tf("the profile “%s” was not found", "профиль «%s» не найден", profile)
}
func ENoProfileHint() string {
	return i18n.T("To list profiles: evo configure list.", "Список профилей — evo configure list.")
}
func TLogoutKept() string {
	return i18n.T("the key of this profile was not created by evo login and remains in the cloud. If you do not need it, delete it in the Cloud.ru personal account",
		"ключ этого профиля создан не через evo login и остается в облаке. Если он не нужен, удалите его в личном кабинете Cloud.ru")
}
func TLogoutDone(profile string, key any) string {
	return i18n.Tf("The profile “%s” was deleted, key: %s. The service account remains in the project",
		"Профиль «%s» удален, ключ: %s. Сервисный аккаунт остался в проекте", profile, key)
}
func eKeyNotDeleted(accessKeyID string, err error) string {
	return i18n.Tf("the access key %s was not deleted: %v; the profile was kept", "ключ доступа %s не удален: %v; профиль сохранен", accessKeyID, err)
}
func eKeyNotDeletedHint() string {
	return i18n.T("Run evo logout again. If the error persists, delete the key in the Cloud.ru personal account (“Service accounts” → the account → “Access keys”) and run evo configure delete <profile>.",
		"Повторите evo logout. Если ошибка повторится, удалите ключ в личном кабинете Cloud.ru («Сервисные аккаунты» → аккаунт → «Ключи доступа») и выполните evo configure delete <профиль>.")
}

// 8. The help of evo login.
func TLoginShort() string {
	return i18n.T("Sign in to Cloud.ru in a browser and save an access key to the profile", "Войти в Cloud.ru через браузер и записать ключ доступа в профиль")
}
func TLoginLong() string {
	return i18n.T(`Sign in to Cloud.ru in a browser and grant access on the Evolution CMCP page
that evo opens on this computer: the project, who the key is for (you:
account evo, the current profile; an agent: account evo-agent, profile
“agent”) and its validity (7, 30 or 90 days). In a terminal, evo asks who
the key is for and its validity before opening the browser. Access is
granted only by the “Grant access” button on the page; there is no --yes
flag. With your rights, evo creates a service account or reuses the
existing one, grants it the role “Project administrator” until the key
expires, creates the key and saves it to the profile. The role allows
deleting any resources of the project and managing service accounts and
keys. The previous key evo login created for this profile is deleted. Your
tokens stay in memory only; the key's secret is never shown. A refusal
names its step and HTTP status. Sign-in does not work over SSH or in a
container. To undo: evo logout.`, `Войти в Cloud.ru через браузер и выдать доступ на странице Evolution CMCP,
которую evo открывает на этом компьютере: проект, для кого ключ (вам —
аккаунт evo, текущий профиль; агенту — аккаунт evo-agent, профиль «agent»)
и срок (7, 30 или 90 дней). Если есть терминал, он до браузера спрашивает,
кому ключ и на какой срок. Доступ выдается только кнопкой «Выдать доступ»
на странице, флага --yes нет. С вашими правами evo создает сервисный
аккаунт или использует уже созданный, выдает ему роль «Администратор
проекта» до конца срока ключа, создает ключ и записывает его в профиль.
Роль дает право удалять любые ресурсы проекта и управлять сервисными
аккаунтами и ключами. Прежний ключ evo login для этого профиля удаляется.
Ваши токены хранятся только в памяти, секрет ключа нигде не показывается.
Отказ называет шаг и HTTP-статус. По SSH и в контейнере вход не пройдет.
Отменить — evo logout.`)
}
func TFlagNoBrowser() string {
	return i18n.T("Do not open a browser; only print the link. Open it on this same computer",
		"Не открывать браузер, только показать ссылку. Открыть ее нужно на этом же компьютере")
}
func TFlagProject() string {
	return i18n.T("Project ID or name: the page shows only this project, selected", "ID или имя проекта: на странице будет только он, отмеченный")
}
func TFlagFor() string {
	return i18n.T("Who the key is for: me (account evo, the current profile) or agent (account evo-agent, profile “agent”)",
		"Для кого ключ: me (аккаунт evo, текущий профиль) или agent (аккаунт evo-agent, профиль «agent»)")
}
func TFlagTTL() string {
	return i18n.T("Key validity: 7d, 30d or 90d (30d by default)", "Срок ключа: 7d, 30d или 90d (по умолчанию 30d)")
}
func EFor(forWhom string) string {
	return i18n.Tf("--for %q: allowed values are me and agent", "--for %q: допустимо me или agent", forWhom)
}
func ETTL(ttl string) string {
	return i18n.Tf("--key-ttl %q: allowed values are 7d, 30d and 90d", "--key-ttl %q: допустимо 7d, 30d или 90d", ttl)
}
func ETTLHint() string { return tForeverInConsole() + "." }
