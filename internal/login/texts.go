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
	return i18n.T("If the project has this account already, no new one is created: the role is extended to the key's term, and the previous key of this profile is revoked.",
		"Если такой аккаунт в проекте уже есть, новый не создаётся: роль продлевается до срока ключа, прежний ключ этого профиля отзывается.")
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

// tDoneText is the result by whom the key is for: an agent goes on by
// itself, a person goes back to the terminal.
func tDoneText(keyID, profile string, term time.Time, forAgent bool) string {
	if forAgent {
		return i18n.Tf("Key %s is kept on this machine, profile «%s». Valid until %s. Go back to the agent: it goes on by itself.",
			"Ключ %s сохранён на этой машине, профиль «%s». Действует до %s. Вернитесь к агенту: он продолжит сам.", keyID, profile, day(term))
	}
	return i18n.Tf("Key %s is kept on this machine, profile «%s». Valid until %s. Go back to the terminal.",
		"Ключ %s сохранён на этой машине, профиль «%s». Действует до %s. Вернитесь в терминал.", keyID, profile, day(term))
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
func eNoRightAny(n int) string {
	return i18n.Tf("creating access takes a project administrator: you have the right in none of your %d projects", "создать доступ может администратор проекта: у вас нет права ни в одном из %d проектов", n)
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
	return i18n.Tf("the right is not confirmed by the platform: %s; the first write answers (a refusal there creates nothing)",
		"право не подтверждено платформой: %s; ответит первая запись платформы (её отказ ничего не создаёт)", reason)
}

// The reasons of tUnchecked: what evaluate answered («Риск», R-49).
func tEvaluateFailed(err string) string {
	return i18n.Tf("evaluate answered %s", "evaluate ответил %s", err)
}
func tActionUnknown(action string) string {
	return i18n.Tf("evaluate answered false on %s; the action is not in the IAM catalog", "evaluate ответил false на %s; действия нет в каталоге IAM", action)
}
func tCatalogUnread(action, err string) string {
	return i18n.Tf("evaluate answered false on %s; the IAM catalog of actions was not read: %s",
		"evaluate ответил false на %s; каталог действий IAM не прочитан: %s", action, err)
}
func eForbidden(project string) string {
	return i18n.Tf("no right to create access in the project %s: the platform refused", "нет права создать доступ в проекте %s: платформа отказала", project)
}

// The errors and notes of the flow, the discovery and the logout.
func eKeyNotDeletedHint() string {
	return i18n.T("Delete the key in the console (Users → Service accounts → the account → Access keys), then 'evo configure delete <profile>'.",
		"Удалите ключ в консоли («Пользователи» → «Сервисные аккаунты» → аккаунт → «Ключи доступа»), затем 'evo configure delete <профиль>'.")
}
func eKeyNotDeleted(accessKeyID string, err error) string {
	return i18n.Tf("the access key %s was not deleted: %v; the profile is kept", "ключ доступа %s не удалён: %v; профиль оставлен", accessKeyID, err)
}
func tUndoRole() string {
	return i18n.T("the role of the account", "роль аккаунта")
}
func tUndoKey() string {
	return i18n.T("the access key", "ключ доступа")
}
func tUndoAccount(account string) string {
	return i18n.Tf("service account %s", "сервисный аккаунт %s", account)
}
func eOrgPages(path string) string {
	return i18n.Tf("Organization API: %s has too many pages", "Organization API: у %s слишком много страниц", path)
}
func eOrgStatus(status int, path string) string {
	return i18n.Tf("Organization API %d at %s", "Organization API %d на %s", status, path)
}
func eOrgUnreachable() string {
	return i18n.T("the Organization API of Cloud.ru is unreachable", "Organization API Cloud.ru недоступен")
}
func eNoProjectHintFlag() string {
	return i18n.T("Pass a project's id or name the person sees in the console, or no --project to choose.",
		"Передайте id или имя проекта, который человек видит в консоли, или не передавайте --project и выберите.")
}
func eNoProjectHint() string {
	return i18n.T("A project is created in the console; or sign in as a member of one.", "Проект создаётся в консоли; или войдите участником проекта.")
}
func eDiscoveryHint() string {
	return i18n.T("This is not a problem of your account: the answer of IAM is not the one evo expects. Try again later; if it repeats, report it.",
		"Это не проблема вашей учётной записи: ответ IAM не тот, что ожидает evo. Повторите позже; если повторится — сообщите об этом.")
}
func eDiscovery(issuer, what string) string {
	return i18n.Tf("the OpenID Connect discovery of %s is not trusted: %s; nothing was sent",
		"discovery OpenID Connect %s не принят: %s; ничего не отправлено", issuer, what)
}
func eNetworkRetry() string {
	return i18n.T("Check the network and try again.", "Проверьте сеть и повторите.")
}
func eOIDCUnreachable() string {
	return i18n.T("the OpenID Connect provider of Cloud.ru is unreachable", "провайдер OpenID Connect Cloud.ru недоступен")
}

// The texts of the commands evo login and evo logout (internal/cli) and
// of evo agent setup about a key of evo login.
func TLogoutDone(profile string, key any) string {
	return i18n.Tf("Profile %q deleted; its key: %s", "Профиль %q удалён; его ключ: %s", profile, key)
}
func TLogoutKept() string {
	return i18n.T("the key of this profile did not come from evo login: it stays in the cloud — delete it in the console",
		"ключ этого профиля создан не evo login: он остаётся в облаке — удалите его в консоли")
}
func TLogoutRevoke(keyID, profile string) string {
	return i18n.Tf("revoke the access key %s and delete the profile %q", "отозвать ключ доступа %s и удалить профиль %q",
		keyID, profile)
}
func TLogoutDelete(profile string) string {
	return i18n.Tf("delete the profile %q", "удалить профиль %q", profile)
}
func ENoProfileHint() string {
	return i18n.T("The profiles: 'evo configure list'.", "Профили: 'evo configure list'.")
}
func ENoProfile(profile string) string {
	return i18n.Tf("no profile %q", "профиля %q нет", profile)
}
func TLogoutLong() string {
	return i18n.T(`Delete the access key evo login made for the profile — by its id, with
its own token — then the profile and its cached token. The service
account and its role stay. A profile whose key came from evo configure or
the console is deleted alone: that key stays in the cloud.`, `Удалить ключ доступа, созданный evo login для профиля, — по его id, его же
токеном, — затем профиль и его токен в кэше. Сервисный аккаунт и роль
остаются. Профиль с ключом из evo configure или консоли удаляется один:
этот ключ остаётся в облаке.`)
}
func TLogoutShort() string {
	return i18n.T("Revoke the access key evo login made and delete the profile", "Отозвать ключ, созданный evo login, и удалить профиль")
}
func ETTLHint() string {
	return i18n.T("A key without a term is created in the console only.", "Бессрочный ключ создаётся только в консоли.")
}
func ETTL(ttl string) string {
	return i18n.Tf("--key-ttl %q: 7d, 30d or 90d", "--key-ttl %q: 7d, 30d или 90d", ttl)
}
func EFor(forWhom string) string {
	return i18n.Tf("--for %q: me or agent", "--for %q: me или agent", forWhom)
}
func TFlagNoBrowser() string {
	return i18n.T("Do not open a browser: only print the address (to open on this same machine)", "Не открывать браузер: только напечатать адрес (открыть на этой же машине)")
}
func TFlagTTL() string {
	return i18n.T("Term of the key: 7d, 30d or 90d (30d by default)", "Срок ключа: 7d, 30d или 90d (по умолчанию 30d)")
}
func TFlagFor() string {
	return i18n.T("Whom the key is for: me (account evo, profile of now) or agent (account evo-agent, profile «agent»)",
		"Для кого ключ: me (аккаунт evo, текущий профиль) или agent (аккаунт evo-agent, профиль «agent»)")
}
func TFlagProject() string {
	return i18n.T("Project id or name to offer alone", "id или имя проекта — будет предложен только он")
}
func TSignedInCLI(keyID, account, project, profile string, until time.Time) string {
	return i18n.Tf("Signed in: the key %s of %s for the project %s is kept in the profile %q until %s",
		"Вход выполнен: ключ %s аккаунта %s для проекта %s записан в профиль %q до %s",
		keyID, account, project, profile, day(until))
}
func TLoginLong() string {
	return i18n.T(`Sign in to Cloud.ru in a browser and grant access on the page evo serves
on this machine: the project, whom the key is for (you: account evo,
profile of now; an agent: account evo-agent, profile «agent») and its
term (7, 30 or 90 days). A terminal, when there is one, asks whom and the
term before the browser; access is granted only by «Grant access» on the
page (no --yes). With your rights evo makes or reuses the service
account, grants it the role «project administrator» until the key's term,
creates the key and keeps it in the profile. The role includes deleting
resources. A previous key evo login made for that profile is revoked.
Your tokens stay in memory; the key's secret is shown nowhere. A refusal
names its step and HTTP status. Over SSH or in a container the sign-in
does not complete. Undo: evo logout.`, `Войти в Cloud.ru через браузер и выдать доступ на странице, которую evo
открывает на этой машине: проект, для кого ключ (вам — аккаунт evo,
текущий профиль; агенту — аккаунт evo-agent, профиль «agent») и его срок
(7, 30 или 90 дней). Терминал, если он есть, спрашивает «кому» и срок до
браузера; доступ выдаёт только кнопка «Выдать доступ» на странице (--yes
нет). Вашими правами evo создаёт или переиспользует сервисный аккаунт,
выдаёт ему роль «Администратор проекта» до срока ключа, создаёт ключ и
записывает его в профиль. Роль включает удаление ресурсов. Прежний ключ
evo login этого профиля отзывается. Ваши токены остаются в памяти; секрет
ключа нигде не показывается. Отказ называет шаг и HTTP-статус. По SSH и в
контейнере вход не пройдёт. Отменить — evo logout.`)
}
func TLoginShort() string {
	return i18n.T("Sign in to Cloud.ru in a browser and keep an access key in the profile", "Войти в Cloud.ru через браузер и записать ключ доступа в профиль")
}
func TAgentKeyWarning(profile string) string {
	return i18n.Tf("the key of the profile %q was made by evo login with the role «%s»: the agent may delete resources with it; an agent nobody watches needs a key whose role cannot delete",
		"ключ профиля %q создан evo login с ролью «%s»: агенту с ним разрешено удаление ресурсов; агенту без присмотра нужен ключ с ролью без права удаления",
		profile, auth.LoginRoleDisplayName)
}

// The preselected project of the consent page (R-48).
func tGrantIn() string {
	return i18n.T("Grant access in the project", "Выдать доступ в проекте")
}
func tPreselectedFrom(source string) string {
	return i18n.Tf("preselected %s — another can be chosen", "подставлен %s — можно выбрать другой", source)
}
func tPreselectNoRight(source string) string {
	return i18n.Tf("the project %s is not checked: no right in it", "проект %s не отмечен: в нём нет права", source)
}
func tPreselectTerminal(project, source string) string {
	return i18n.Tf("The page offers the project %s, %s.", "На странице отмечен проект %s, %s.", project, source)
}

// TFromProfile and TFromEnv name where a preselected project came from.
func TFromProfile(profile string) string {
	return i18n.Tf("from the evo profile «%s»", "из профиля evo «%s»", profile)
}
func TFromEnv() string { return i18n.T("from CLOUDRU_PROJECT_ID", "из CLOUDRU_PROJECT_ID") }
