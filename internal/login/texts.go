// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Every text of evo login and logout is here, from the second edition of
// «Fable — авторизация» (05.11): the terminal and the page say the same
// summary, errors and result (contract B-1, invariant 11), and a new
// edition is a change of these strings, not of the code (the owner's
// decision 05.11, #986).

// day is a date as the texts write it: 05.12.2026.
func day(t time.Time) string { return t.Local().Format("02.01.2006") }

func tHeading() string {
	return i18n.T("evo access to a project", "Доступ evo к проекту")
}

func tSignedIn(login string) string {
	return i18n.Tf("You signed in as %s. evo will create a service account and a key with which it manages the cloud in the project you choose.",
		"Вы вошли как %s. evo создаст сервисный аккаунт и ключ, которыми будет управлять облаком в выбранном проекте.", login)
}

func tOpen() string {
	return i18n.T("Sign in to Cloud.ru in the browser. The link, if the browser did not open:",
		"Вход в Cloud.ru через браузер. Ссылка, если браузер не открылся:")
}

func tWaiting(wait time.Duration) string {
	return i18n.Tf("Waiting for the sign-in (up to %d min, Ctrl-C to cancel)…", "Жду вход (до %d мин, Ctrl-C — отмена)…", int(wait.Minutes()))
}

func tProject() string { return i18n.T("Project", "Проект") }
func tNoRightMark() string {
	return i18n.T("no right: takes the project administrator", "нет права: нужен администратор проекта")
}
func tForWhom() string { return i18n.T("Whom the key is for", "Кому ключ") }
func tChoose() string  { return i18n.T("Choose (number)", "Выберите (номер)") }
func tTermQuestion() string {
	return i18n.T("Term of the key, days (7, 30, 90)", "Срок ключа, дней (7, 30, 90)")
}
func tTerm() string { return i18n.T("Term of the key", "Срок ключа") }
func tWhatWillBeDone() string {
	return i18n.T("What will be done:", "Что будет сделано:")
}
func tWhatWillBeMade() string {
	return i18n.T("What will be created", "Что будет создано")
}
func tGrant() string  { return i18n.T("Grant access", "Выдать доступ") }
func tCancel() string { return i18n.T("Cancel", "Отмена") }

func tNoRightNote() string {
	return i18n.T("Access in a project without the right can be created by its administrator: ask them to run evo login, or to create a key in the console and pass it to you.",
		"Доступ в проекте без права может создать его администратор: попросите его выполнить evo login или создать ключ в консоли и передать вам.")
}

func tForMe() string    { return i18n.T("Me", "Мне") }
func tForAgent() string { return i18n.T("An agent", "Агенту") }
func tForMeNote() string {
	return i18n.T("Account evo, commands in the terminal.", "Аккаунт evo, команды в терминале.")
}
func tForAgentNote() string {
	return i18n.T("Account evo-agent, the key of an MCP server.", "Аккаунт evo-agent, ключ для MCP-сервера.")
}

func tRoleNote() string {
	return i18n.Tf("Both accounts get the role «%s», including deleting resources and managing keys.",
		"Оба аккаунта получают роль «%s», включая удаление ресурсов и управление ключами.", auth.LoginRoleDisplayName)
}

func tDays(n int) string { return i18n.Tf("%d days", "%d дней", n) }
func tForeverInConsole() string {
	return i18n.T("A key without a term: in the console only.", "Бессрочный ключ: только в консоли.")
}

// Summary lines of the terminal and the page: the same words.
func tSumAccountNew(account, project string) string {
	return i18n.Tf("Account %s in the project %s: create.", "Аккаунт %s в проекте %s: создать.", account, project)
}
func tSumRole(until string) string {
	return i18n.Tf("Role «%s» until %s.", "Роль «%s» до %s.", auth.LoginRoleDisplayName, until)
}
func tSumKey(until string) string { return i18n.Tf("Key until %s.", "Ключ до %s.", until) }
func tSumStays() string {
	return i18n.T("The account stays after logout; the role expires with the key.", "Аккаунт остаётся после выхода, роль истекает вместе с ключом.")
}
func tSumReuseNote() string {
	return i18n.T("If the account is there, evo writes instead of «create»: «there, keys N of 50»; the role is extended to the term, and the previous key of this machine is revoked by its id.",
		"Если аккаунт есть, evo пишет вместо «создать»: «есть, ключей N из 50»; роль продлевается до срока, прежний ключ этой машины отзывается по его id.")
}
func tSecretNote() string {
	return i18n.T("The key's secret is not shown: evo on this computer creates the account and the key with your rights and keeps the key in the profile.",
		"Секрет ключа не показывается: evo на этом компьютере создаёт аккаунт и ключ вашими правами и записывает ключ в профиль.")
}

// The terminal's summary rows.
func tRowAccount() string { return i18n.T("account", "аккаунт") }
func tRowRole() string    { return i18n.T("role", "роль") }
func tRowKey() string     { return i18n.T("key", "ключ") }
func tRowProfile() string { return i18n.T("profile", "профиль") }
func tRoleUntil(term time.Time) string {
	return i18n.Tf("%s, until %s", "%s, до %s", auth.LoginRoleDisplayName, day(term))
}
func tKeyDays(days int, term time.Time) string {
	return i18n.Tf("%d days, until %s", "%d дней, до %s", days, day(term))
}

// Steps of the terminal.
func tStepAccount(account string, reused bool) string {
	if reused {
		return i18n.Tf("account %s… there", "аккаунт %s… есть", account)
	}
	return i18n.Tf("account %s… created", "аккаунт %s… создан", account)
}
func tStepRole() string     { return i18n.T("role… granted", "роль… выдана") }
func tStepKey() string      { return i18n.T("key… issued", "ключ… выпущен") }
func tStepKeyCheck() string { return i18n.T("key check… ok", "проверка ключа… ок") }

// The result.
func tDone() string { return i18n.T("Done", "Готово") }
func tDoneText(keyID, profile string, term time.Time) string {
	return i18n.Tf("Key %s is kept on this machine, profile «%s». Valid until %s. Go back to the agent: it goes on by itself.",
		"Ключ %s сохранён на этой машине, профиль «%s». Действует до %s. Вернитесь к агенту: он продолжит сам.", keyID, profile, day(term))
}
func tReused() string {
	return i18n.T("there, used again", "есть, использован повторно")
}
func tRevokedPrevious(keyID string) string {
	return i18n.Tf("the previous key %s of this machine is revoked", "прежний ключ %s на этой машине отозван", keyID)
}
func tRevokeHow() string {
	return i18n.T("To revoke: evo logout or the console → Service accounts. This window can be closed.",
		"Отзыв: evo logout или консоль → Сервисные аккаунты. Это окно можно закрыть.")
}
func tCancelledTitle() string { return i18n.T("Cancelled", "Отменено") }
func tCancelled() string {
	return i18n.T("Cancelled, nothing was created. Run evo login again when ready.", "Отменено, ничего не создано. Повторите evo login, когда будете готовы.")
}
func tNotComplete() string {
	return i18n.T("Access not complete", "Доступ не завершён")
}
func tDetails(reason string, code int) string {
	return i18n.Tf("Details and the exit code are in the terminal (%s, %d). Run evo login again. This window can be closed.",
		"Подробности и код выхода — в терминале (%s, %d). Повторить — evo login. Это окно можно закрыть.", reason, code)
}
func tForeign() string {
	return i18n.T("This request is not of this sign-in. Run evo login again.", "Запрос не от этого входа. Повторите evo login.")
}

// The errors (one text in the terminal and on the page).
func eCancelled() string {
	return i18n.T("the sign-in was cancelled, nothing was created", "вход отменён, ничего не создано")
}
func eSession(status int) string {
	return i18n.Tf("the sign-in is not complete: the session expired (HTTP %d)", "вход не завершён: сессия истекла (HTTP %d)", status)
}
func eIDToken(what string) string {
	return i18n.Tf("IAM did not confirm the sign-in (ID token: %s), nothing was created", "вход не подтверждён IAM (ID token: %s), ничего не создано", what)
}
func eIDTokenHint() string {
	return i18n.T("Run evo login again; if it repeats, write to support.", "Повторите evo login; если повторяется, напишите в поддержку.")
}
func eNoRight(project string) string {
	return i18n.Tf("creating access takes the administrator of the project %s", "создать доступ может администратор проекта %s", project)
}
func eNoRightHint() string {
	return i18n.T("Ask them to run evo login, or to create a key in the console and pass it to you.",
		"Попросите его выполнить evo login или создать ключ в консоли и передать вам.")
}
func eAccountLimit(project string, n int) string {
	return i18n.Tf("the project %s has %d service accounts already, no new one can be created", "в проекте %s уже %d сервисных аккаунтов, новый создать нельзя", project, n)
}
func eAccountLimitHint() string {
	return i18n.T("Delete one no one uses in the console.", "Удалите ненужный в консоли.")
}
func eKeyLimit(account string, n int) string {
	return i18n.Tf("the account %s has %d keys already", "у аккаунта %s уже %d ключей", account, n)
}
func eKeyLimitHint(account string) string {
	return i18n.Tf("Revoke the ones no one uses: the console → Service accounts → %s.", "Отзовите ненужные: консоль → Сервисные аккаунты → %s.", account)
}
func eNotComplete(cause string, made, undone, left []string) string {
	return i18n.Tf("access is not complete: %s; created: %v; undone: %v; left: %v", "доступ не завершён: %s; создано: %v; отменено: %v; осталось: %v",
		cause, made, undone, left)
}
func eNotCompleteHint(left bool) string {
	if left {
		return i18n.T("Delete what is left in the console → Service accounts, then run evo login again.",
			"Удалите оставшееся в консоли → Сервисные аккаунты и повторите evo login.")
	}
	return i18n.T("Nothing of this login is left. Run evo login again.", "От этого входа ничего не осталось. Повторите evo login.")
}
func ePorts(ports []int) string {
	return i18n.Tf("ports %v are taken", "порты %v заняты", ports)
}
func ePortsHint() string {
	return i18n.T("Free one of them and run evo login again.", "Освободите один и повторите evo login.")
}
func eTimeout(wait time.Duration) string {
	return i18n.Tf("the sign-in was not done in %d minutes, nothing was created", "вход не выполнен за %d минут, ничего не создано", int(wait.Minutes()))
}
func eProvider(what string) string {
	return i18n.Tf("the provider of Cloud.ru refused the sign-in: %s; nothing was created", "провайдер входа Cloud.ru отказал: %s; ничего не создано", what)
}
func eKeyCheck(status int) string {
	return i18n.Tf("the new key did not get an IAM token (HTTP %d)", "новый ключ не получил токен IAM (HTTP %d)", status)
}
func eChoice(what string) string {
	return i18n.Tf("the choice is not one offered (%s), nothing was created", "выбор не из предложенных (%s), ничего не создано", what)
}
func eNoProject(project, login string) string {
	if project == "" {
		return i18n.Tf("%s sees no project, nothing was created", "%s не видит ни одного проекта, ничего не создано", login)
	}
	return i18n.Tf("no project %q among the projects of %s, nothing was created", "проекта %q нет среди проектов %s, ничего не создано", project, login)
}
func eRetry() string { return i18n.T("Run evo login again.", "Повторите evo login.") }
func eKeyLeft(keyID string, err error) string {
	return i18n.Tf("the previous key %s is left: %v — revoke it in the console", "прежний ключ %s остался: %v — отзовите его в консоли", keyID, err)
}

// tAt names the step of a refusal and the HTTP status of the answer that
// made it, when one did (#981).
func tAt(step string, status int) string {
	if status != 0 {
		return i18n.Tf(" (step %s, HTTP %d)", " (шаг %s, HTTP %d)", step, status)
	}
	return i18n.Tf(" (step %s)", " (шаг %s)", step)
}
func eProviderHint() string {
	return i18n.T("Run evo login again; if it repeats, the account may not sign in here — ask the organization administrator.",
		"Повторите evo login; если повторится — входу этой учётной записи что-то мешает, спросите администратора организации.")
}
func eCancelledHint() string {
	return i18n.T("Run evo login when ready.", "Повторите evo login, когда будете готовы.")
}
func tGrantOnPage() string {
	return i18n.T("The project is chosen on the page that opens after the sign-in; access is granted only by «Grant access» there.",
		"Проект выбирается на странице, которая откроется после входа; доступ выдаёт только кнопка «Выдать доступ» на ней.")
}
func tNoBrowser() string {
	return i18n.T("The browser is not opened: open the link on this same machine. Over SSH or in a container the sign-in does not complete: the provider returns to 127.0.0.1 of the machine with the browser.",
		"Браузер не открывается: откройте ссылку на этой же машине. По SSH и в контейнере вход не пройдёт: провайдер возвращает на 127.0.0.1 машины с браузером.")
}
func eNoYes() string {
	return i18n.T("evo login takes no --yes: access is granted only by «Grant access» on the page", "у evo login нет --yes: доступ выдаёт только кнопка «Выдать доступ» на странице")
}
func eNoYesHint() string {
	return i18n.T("Run evo login without --yes; the person grants on the page.", "Запустите evo login без --yes; доступ выдаёт человек на странице.")
}

// RefuseYes is the refusal of evo login --yes, before any request: the
// consent is the page's («Риск», conclusion 24).
func RefuseYes() error { return clierrors.New(clierrors.CodeUsage, eNoYes(), eNoYesHint()) }

func tUnchecked(reason string) string {
	return i18n.Tf("the right is not checked: %s; the platform answers the first write (a refusal there creates nothing)",
		"право не проверено: %s; ответит первая запись платформы (её отказ ничего не создаёт)", reason)
}
