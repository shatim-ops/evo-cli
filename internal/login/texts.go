// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"strings"
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

// tPageUntil: after the wait the page's button meets a closed loopback.
func tPageUntil(t time.Time) string {
	clock := t.Local().Format("15:04")
	return i18n.Tf("This page works until %s. Later, go back to the terminal and run evo login again",
		"Страница действует до %s. Позже вернитесь в терминал и запустите evo login снова", clock)
}

// tChooseProject is the empty first option of the list of projects.
func tChooseProject() string { return i18n.T("Choose a project", "Выберите проект") }
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
	return i18n.T("If you are not a project administrator, ask the project administrator to create a service account with the required role and a key for you in the Cloud.ru console",
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
	return i18n.Tf("The role “%s” gives the agent full access to the project: it can create, modify and delete any resources of the project, including those not created through %s, and manage service accounts and keys. The role is granted until %s. To revoke access earlier, sign out of %s on this computer — the key will be deleted. The role is removed in the Cloud.ru console",
		"Роль «%s» дает агенту полный доступ к ресурсам проекта: он может создавать, изменять и удалять любые ресурсы проекта, в том числе созданные не через %s, а также управлять сервисными аккаунтами и ключами. Роль выдается со сроком до %s. Чтобы отозвать доступ раньше, выйдите из %s на этом компьютере — ключ будет удален. Роль снимается в личном кабинете Cloud.ru",
		auth.LoginRoleDisplayName, product.PageName, until, product.PageName)
}
func tTerm() string      { return i18n.T("Key validity", "Срок ключа") }
func tDays(n int) string { return i18n.Tf("%d days", "%d дней", n) }
func tForeverInConsole() string {
	return i18n.T("Keys that never expire can be created only in the Cloud.ru console", "Бессрочные ключи создаются только в личном кабинете Cloud.ru")
}

// tUnchecked is the one mark of the right on the page and in the terminal
// (conclusions 26.5 and 28.1): what evaluate answered, by the list.
// tUnchecked: the platform did not confirm the right beforehand; its
// reason is in the details, not here («Риск», conclusion 35.4).
func tUnchecked() string {
	return i18n.T("The right could not be checked beforehand. If there is no access, Cloud.ru will refuse to create the account, and nothing will be left behind",
		"Заранее проверить право не удалось. Если доступа нет, Cloud.ru откажет при создании аккаунта, и ничего не останется")
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

// tSumAccountToChoose and tGrant: the summary and the button before a
// project is chosen, or where the browser cannot name the chosen one.
func tSumAccountToChoose(account string) string {
	return i18n.Tf("The service account %s will be created in the project you choose (or the existing one reused).",
		"Сервисный аккаунт %s в проекте, который вы выберете, будет создан (или использован уже созданный).", account)
}
func tSumRole(until string) string {
	return i18n.Tf("The role “%s” will be granted until %s. If the account already holds it for longer or with no end date, it stays so; the result will name its term.",
		"Роль «%s» будет выдана до %s. Если у аккаунта она уже выдана на дольше или бессрочно, она останется такой — срок назовет итог.", auth.LoginRoleDisplayName, until)
}
func tSumKey(until string) string {
	return i18n.Tf("The access key will be valid until %s.", "Ключ доступа будет действовать до %s.", until)
}

// tSumStaysPage: whether the account is new is known
// only after the page (step 12), so the role «expires in its term», not
// «with the key» («Риск», conclusion 28.3).
func tSumStaysPage() string {
	return i18n.Tf("After you sign out of %s, the account remains and the role expires at the end of its term",
		"После выхода из %s аккаунт останется, а роль истечет в свой срок", product.PageName)
}

// tGrantInProject is the main button; the page puts the chosen project's
// name where %s is, by CSS.
func tGrant() string { return i18n.T("Grant access", "Выдать доступ") }
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
// when there was no previous key. role is tRoleTerm.
func tDoneReused(account, role, previousKey string) string {
	if previousKey == "" {
		return i18n.Tf("The existing service account %s was reused: the role “%s” is valid %s",
			"Использован существующий сервисный аккаунт %s: роль «%s» действует %s", account, auth.LoginRoleDisplayName, role)
	}
	return i18n.Tf("The existing service account %s was reused: the role “%s” is valid %s; the previous key %s was deleted in Cloud.ru",
		"Использован существующий сервисный аккаунт %s: роль «%s» действует %s, прежний ключ %s удален в Cloud.ru",
		account, auth.LoginRoleDisplayName, role, previousKey)
}

// The row names of the result page; the terminal's rows are tRow*.
func tRowsPage() [5]string {
	return [5]string{i18n.T("Project", "Проект"), i18n.T("Account", "Аккаунт"), i18n.T("Role", "Роль"),
		i18n.T("Key", "Ключ"), i18n.T("Profile", "Профиль")}
}
func tRoleUntil(role string) string {
	return i18n.Tf("%s, %s", "%s, %s", auth.LoginRoleDisplayName, role)
}

// tRoleTerm is the role's actual term: a date, or none — an unbounded role
// the account held stays so (A18-26).
func tRoleTerm(until time.Time, unbounded bool) string {
	if unbounded {
		return i18n.T("with no end date", "бессрочно")
	}
	return i18n.Tf("until %s", "до %s", day(until))
}
func tRevokeHow() string {
	return i18n.Tf("To revoke access, sign out of %s or delete the key in the Cloud.ru console under “Service accounts”. You can close this window",
		"Чтобы закрыть доступ, выйдите из %s или удалите ключ в личном кабинете Cloud.ru в разделе «Сервисные аккаунты». Это окно можно закрыть",
		product.PageName)
}

// 3. «Cancelled» and the error page: the page shows the message, the hint
// with its command stays the terminal's and JSON's.

func tCancelledTitle() string { return i18n.T("Cancelled", "Отменено") }
func tCancelled() string {
	return i18n.T("Nothing was created. To start over, go back to the terminal and run evo login again. You can close this window",
		"Ничего не создано. Чтобы начать заново, вернитесь в терминал и запустите evo login снова. Это окно можно закрыть")
}
func tNotComplete() string {
	return i18n.T("Access could not be granted", "Не удалось выдать доступ")
}

// tSignInFailed, tNotGranted and tNothingMade: the error page of a step
// before any write («Риск», conclusion 57).
func tSignInFailed() string { return i18n.T("Sign-in failed", "Вход не выполнен") }
func tNotGranted() string   { return i18n.T("Access not granted", "Доступ не выдан") }
func tNothingMade() string {
	return i18n.T("No access was granted: evo created nothing", "Доступ не выдавался: evo ничего не создал")
}
func tDetails(reason string, code int) string {
	return i18n.Tf("See the details (%s, code %d) in the terminal where you started the sign-in. To try again, go back to the terminal and run evo login again. You can close this window",
		"Подробности (%s, код %d) — в терминале, где запускали вход. Чтобы повторить, вернитесь в терминал и запустите evo login снова. Это окно можно закрыть", reason, code)
}

// tAlreadyOpen is the answer to a second callback of this login: the link
// opened again (or reloaded) after the first one took the code. The page
// of the first one works — «sign in again» burned it (release audit 1.8,
// L5, Grok R1b №7).
func tAlreadyOpen() string {
	return i18n.T("This sign-in is already under way: go back to the page of evo opened first in this browser (or to the terminal). A second evo login does not start while this one waits (STATE_LOCKED); to start over, finish or cancel this one in the terminal",
		"Этот вход уже идет: вернитесь к странице evo, открытой первой в этом браузере (или в терминал). Второй evo login не начнется, пока идет этот (STATE_LOCKED); чтобы начать заново — завершите или отмените этот в терминале")
}

// tForeign answers a request that is not of this login. It sends back to
// the login that waits, not to a second one: that does not start while
// this one waits (STATE_LOCKED; «Риск», conclusion 58, № 22). The Host,
// the Origin and the token of the consent share it: a forgery learns
// nothing of which check failed (R-47).
func tForeign() string {
	return i18n.T("This request does not belong to the current sign-in of evo. Go back to the page the sign-in opened, or to the terminal",
		"Запрос не относится к текущему входу evo. Вернитесь к странице, которую открыл вход, или в терминал")
}

// tForeignLink answers the provider's redirect with another login's
// state: a link of another or an earlier sign-in (№ 22).
func tForeignLink() string {
	return i18n.T("This link is from another or an earlier sign-in of evo. The current sign-in waits in the terminal: go back to the page it opened, or cancel it in the terminal and run evo login again",
		"Эта ссылка — от другого или более раннего входа evo. Текущий вход ждет в терминале: вернитесь к странице, которую он открыл, или отмените его в терминале и запустите evo login снова")
}

// 4. The terminal.

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
	return i18n.T("To revoke earlier: evo logout deletes the key; the role is removed in the Cloud.ru console",
		"Отозвать раньше: evo logout удалит ключ, роль снимается в личном кабинете Cloud.ru")
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

// tCannotReach and tOverSSH: a browser that cannot reach 127.0.0.1 of
// this computer ends at «cannot connect» after sign-in (P1).
func tCannotReach() string {
	return i18n.T("If after sign-in the browser cannot open 127.0.0.1, it is not on this computer or runs in a sandbox: sign in where evo and the browser are on one computer",
		"Если после входа браузер не может открыть 127.0.0.1, он не на этом компьютере или работает в песочнице: выполните вход там, где evo и браузер на одном компьютере")
}
func tOverSSH(port int) string {
	return i18n.Tf("This terminal is over SSH: a browser on your computer cannot return to 127.0.0.1 of this one. Sign in on your computer, or forward the port first: ssh -L %d:127.0.0.1:%d <this host>",
		"Терминал открыт по SSH: браузер вашего компьютера не вернется на 127.0.0.1 этого. Выполните вход на своем компьютере или сначала пробросьте порт: ssh -L %d:127.0.0.1:%d <этот хост>", port, port)
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
func TResultRole(until time.Time, unbounded bool) string {
	return i18n.Tf("The role “%s” is valid %s", "Роль «%s» действует %s", auth.LoginRoleDisplayName, tRoleTerm(until, unbounded))
}
func TResultReused(account, previousKey string) string {
	if previousKey == "" {
		return i18n.Tf("The existing service account %s was reused", "Использован существующий сервисный аккаунт %s", account)
	}
	return i18n.Tf("The existing service account %s was reused; the previous key %s was deleted in Cloud.ru",
		"Использован существующий сервисный аккаунт %s, прежний ключ %s удален в Cloud.ru", account, previousKey)
}
func TResultCheck(profile string) string {
	return i18n.Tf("Check: evo whoami --profile %s", "Проверка: evo whoami --profile %s", profile)
}

// TResultForAgent: after a key for «Мне», the way to a key for an agent
// («Риск», addition to conclusion 34).
func TResultForAgent() string {
	return i18n.T("A key for an agent: evo login --for agent", "Ключ агенту: evo login --for agent")
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

// RefuseDryRun: evo login has no preview — what it would do is the page's
// to show and the person's to grant (plan 1.8, #1095, rounds 1–2).
func RefuseDryRun() error {
	return clierrors.New(clierrors.CodeUsage,
		i18n.T("evo login has no --dry-run: what is granted is shown and chosen on the consent page, nothing is made before the button",
			"у evo login нет --dry-run: что будет выдано, показывает и выбирает страница согласия, до кнопки ничего не создается"),
		i18n.T("Run evo login without --dry-run; to leave without granting, press “Cancel” on the page.",
			"Запустите evo login без --dry-run; чтобы уйти, ничего не выдав, нажмите «Отмена» на странице."))
}

// RefuseScopeFlag: evo login takes no --project-id or --region of the
// other commands; it skipped them silently, and evo login --project-id
// looked like a choice of the project (plan 1.8, #1095, round 4).
func RefuseScopeFlag(flag string) error {
	if flag == "project-id" {
		return clierrors.New(clierrors.CodeUsage,
			i18n.T("evo login has no --project-id: the project is chosen on the consent page",
				"у evo login нет --project-id: проект выбирается на странице согласия"),
			i18n.T("To show only one project on the page, selected: evo login --project <ID or name>.",
				"Чтобы на странице был только нужный проект, отмеченный: evo login --project <ID или имя>."))
	}
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("evo login has no --%s: it grants access to a project, not to a zone", "у evo login нет --%s: вход выдаёт доступ к проекту, а не к зоне", flag),
		i18n.T("Run evo login without it; the zone is given to the commands that create resources (--zone or --region).",
			"Запустите evo login без него; зону задают командам, которые создают ресурсы (--zone или --region)."))
}

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
	return i18n.T("Ask the project administrator to create a service account with the required role and a key for you in the Cloud.ru console. Retrying will not help.",
		"Попросите администратора проекта создать для вас сервисный аккаунт с нужной ролью и ключ в личном кабинете Cloud.ru. Повтор не поможет.")
}

// eForbidden: the platform's 403 at the first write («Риск», R-49).
func eForbidden(project string) string {
	return i18n.Tf("not enough rights to grant access in the project “%s”: Cloud.ru refused", "недостаточно прав для выдачи доступа в проекте «%s»: Cloud.ru отказал", project)
}
func eNotComplete(cause string, made, undone, left []string) string {
	return i18n.Tf("access was not granted: %s; created: %s; rolled back: %s; left behind: %s", "доступ не выдан: %s; создано: %s; отменено: %s; осталось: %s",
		cause, listed(made), listed(undone), listed(left))
}

// listed is a list of what a rollback met, by commas; an empty one is
// «nothing», not «[]» (finding 24 of 1.8, «Риск», conclusion 58).
func listed(what []string) string {
	if len(what) == 0 {
		return i18n.T("nothing", "ничего")
	}
	return strings.Join(what, ", ")
}
func eNotCompleteHint(left bool, next string) string {
	if left {
		return i18n.T("Delete what was left behind in the Cloud.ru console under “Service accounts” and run evo login again.",
			"Удалите оставшееся в личном кабинете Cloud.ru в разделе «Сервисные аккаунты» и повторите evo login.")
	}
	return tNothingLeft() + " " + next
}
func tNothingLeft() string {
	return i18n.T("Nothing was left behind.", "Ничего не осталось.")
}

// What Cloud.ru did not do at a write: the first words of its refusal,
// the platform's own follow («Риск», conclusion 35.3).
func eNotCreatedAccount() string {
	return i18n.T("Cloud.ru did not create the service account", "Cloud.ru не создал сервисный аккаунт")
}
func eNotGrantedRole() string {
	return i18n.Tf("Cloud.ru did not grant the role “%s”", "Cloud.ru не выдал роль «%s»", auth.LoginRoleDisplayName)
}
func eNotIssuedKey() string {
	return i18n.T("Cloud.ru did not issue the access key", "Cloud.ru не выдал ключ доступа")
}
func eRoleRefusedHint() string {
	return i18n.T("Cloud.ru refused for the reason above; a retry alone meets the same refusal. Choose another project or contact the administrator of the organization.",
		"Cloud.ru отказал по причине выше; простой повтор получит тот же отказ. Выберите другой проект или обратитесь к администратору организации.")
}
func eAccountRefusedHint() string {
	return i18n.T("Cloud.ru refused to create the service account for the reason above; a retry alone meets the same refusal. Choose another project or contact the administrator of the organization.",
		"Cloud.ru отказал создать сервисный аккаунт по причине выше; простой повтор получит тот же отказ. Выберите другой проект или обратитесь к администратору организации.")
}
func eKeyRefusedHint(account string) string {
	return i18n.Tf("Cloud.ru refused to issue the key for the reason above; a retry alone meets the same refusal. The keys of the account %s may serve other machines: contact the administrator of the organization.",
		"Cloud.ru отказал выдать ключ по причине выше; простой повтор получит тот же отказ. Ключи аккаунта %s могут работать на других машинах — обратитесь к администратору организации.", account)
}
func ePorts(ports []int) string {
	return i18n.Tf("ports %v are in use; nothing was created", "порты %v заняты, ничего не создано", ports)
}
func ePortsHint() string {
	return i18n.T("Free one of the ports and run evo login again.", "Освободите один из портов и повторите evo login.")
}
func eTimeout(wait time.Duration) string {
	return i18n.Tf("the wait is over: sign-in was not completed within %s; nothing was created",
		"время ожидания истекло: вход не выполнен за %s, ничего не создано", waitText(wait))
}
func eTimeoutHint(wait time.Duration) string {
	return i18n.Tf("Run evo login again and finish it in the browser within %s; the page of this sign-in no longer works.",
		"Запустите evo login снова и завершите вход в браузере за %s; страница этого входа больше не работает.", waitText(wait))
}

// waitText is the wait in minutes, in seconds when shorter than one: never
// «0 min».
func waitText(wait time.Duration) string {
	if wait < time.Minute {
		return i18n.Tf("%d s", "%d с", int(wait.Seconds()))
	}
	return i18n.Tf("%d min", "%d мин", int(wait.Minutes()))
}

// eLoginRunning: another evo login waits for the browser on this computer
// («Риск», conclusion 35.1: no advice to kill it).
// eLoginRunning names the configuration directory the lock is on: one
// login per directory, not per computer — another CLOUDRU_CONFIG_DIR has
// its own (D55; findings ledger 1.8, № 131).
func eLoginRunning(dir string, port int) string {
	if port == 0 {
		return i18n.Tf("another evo login with the configuration directory %s is waiting in the browser; nothing was started",
			"другой evo login с каталогом настроек %s ждет в браузере, новый вход не начат", dir)
	}
	return i18n.Tf("another evo login with the configuration directory %s is waiting in the browser (port %d); nothing was started",
		"другой evo login с каталогом настроек %s ждет в браузере (порт %d), новый вход не начат", dir, port)
}
func eLoginRunningHint() string {
	return i18n.T("Finish or cancel that sign-in, then run evo login again.",
		"Завершите или отмените тот вход, затем запустите evo login снова.")
}

// eProvider is the provider's error at the callback. A known code of OAuth
// reads as words, the code and the provider's description in brackets for
// the support; an unknown one as the provider sent it (№ 23, «Риск»,
// conclusion 58). The description is cut and quoted (oneLine), the page
// escapes it.
func eProvider(code, description string) string {
	what := oneLine(code + ": " + description)
	if known, ok := oauthErrors[code]; ok {
		raw := code
		if description != "" {
			raw += ": " + oneLine(description)
		}
		what = known.words() + " (" + raw + ")"
	}
	return i18n.Tf("Cloud.ru rejected the sign-in: %s; nothing was created", "Cloud.ru отклонил вход: %s; ничего не создано", what)
}

// oauthError is a code of an error of the authorization request in words,
// and its hint when it is not the general one: unauthorized_client and
// invalid_scope are of the client evo in Cloud.ru, not of the person's
// account («Риск», conclusion 63).
type oauthError struct{ words, hint func() string }

// oauthErrors are the codes of an error of the authorization request
// (RFC 6749, 4.1.2.1; OpenID Connect Core, 3.1.2.6); access_denied is the
// cancel, not an error.
var oauthErrors = map[string]oauthError{
	"invalid_request": {words: func() string {
		return i18n.T("the sign-in request was not accepted", "запрос на вход не принят")
	}},
	"unauthorized_client": {words: func() string {
		return i18n.T("evo may not sign in this way", "evo не разрешено входить этим способом")
	}, hint: eClientHint},
	"invalid_scope": {words: func() string {
		return i18n.T("the access evo asks for is not allowed", "доступ, который запрашивает evo, не разрешен")
	}, hint: eClientHint},
	"server_error": {words: func() string {
		return i18n.T("an error on the side of Cloud.ru", "ошибка на стороне Cloud.ru")
	}},
	"temporarily_unavailable": {words: func() string {
		return i18n.T("the sign-in is unavailable for now", "вход временно недоступен")
	}},
	"login_required": {words: func() string {
		return i18n.T("you need to sign in to Cloud.ru again", "нужно заново войти в Cloud.ru")
	}},
	"interaction_required": {words: func() string {
		return i18n.T("Cloud.ru needs you to confirm the sign-in in the browser", "Cloud.ru просит подтвердить вход в браузере")
	}},
	"consent_required": {words: func() string {
		return i18n.T("Cloud.ru needs your consent to the sign-in", "Cloud.ru просит вашего согласия на вход")
	}},
}

// eProviderHint is the hint of the provider's error: its code's own, or
// the general one.
func eProviderHint(code string) string {
	if known, ok := oauthErrors[code]; ok && known.hint != nil {
		return known.hint()
	}
	return i18n.T("Run evo login again. If the error persists, ask your organization administrator to check the sign-in settings of your account.",
		"Повторите evo login. Если ошибка повторится, обратитесь к администратору организации: он проверит настройки входа вашей учетной записи.")
}

// eClientHint: the client evo is not accepted as it signs in — not the
// account; an evo of a later version may be, else the support.
func eClientHint() string {
	return i18n.T("This code points to the setup of evo in Cloud.ru, not to your account. Update evo to the latest version; if the error persists, report it to Cloud.ru support with the code in brackets.",
		"Этот код указывает на настройку evo в Cloud.ru, а не на вашу учетную запись. Обновите evo до последней версии; если ошибка повторится, сообщите о ней в поддержку Cloud.ru с кодом из скобок.")
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
	return i18n.T("Create a project in the Cloud.ru console, or sign in as a project member.",
		"Создайте проект в личном кабинете Cloud.ru или войдите как участник проекта.")
}
func eNoProjectHintFlag() string {
	return i18n.T("Pass in --project the ID or name of a project from the Cloud.ru console, or run evo login without --project to choose the project on the page.",
		"Укажите в --project ID или имя проекта из личного кабинета Cloud.ru или запустите evo login без --project, чтобы выбрать проект на странице.")
}
func eKeyLeft(keyID string, err error) string {
	return i18n.Tf("the previous key %s was not deleted: %v. Delete it in the Cloud.ru console", "прежний ключ %s не удален: %v. Удалите его в личном кабинете Cloud.ru", keyID, err)
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

// The items of «created / undone / left».
func tUndoAccount(account string) string {
	return i18n.Tf("service account %s", "сервисный аккаунт %s", account)
}
func tUndoKey() string  { return i18n.T("access key", "ключ доступа") }
func tUndoRole() string { return i18n.T("the account's role", "роль аккаунта") }
func tUndoRoleTerm() string {
	return i18n.T("the term of the account's role", "срок роли аккаунта")
}

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
profile's key was created by evo configure or in the Cloud.ru console, only
the profile is deleted and the key remains in the cloud.`, `Удалить ключ доступа, который evo login создал для профиля, затем сам
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
	return i18n.T("the key of this profile was not created by evo login and remains in the cloud: evo leaves it as it is",
		"ключ этого профиля создан не через evo login и остается в облаке: evo его не трогает")
}
func TLogoutDone(profile string, key any) string {
	return i18n.Tf("The profile “%s” was deleted, %s. The service account remains in the project",
		"Профиль «%s» удален, %s. Сервисный аккаунт остался в проекте", profile, keyWords(key))
}

// keyWords is the status of the key in logout's answer, in words: the
// line said «key: deleted» in English (plan 1.8, B-5 round 3 (в)).
func keyWords(key any) string {
	switch key {
	case KeyDeleted:
		return i18n.T("the key was deleted", "ключ удален")
	case KeyAlreadyDeleted:
		return i18n.T("the key was deleted already", "ключа уже не было")
	case KeyRejected:
		return i18n.T("the key was not deleted: IAM does not accept it", "ключ не удален: IAM его не принимает")
	}
	return i18n.T("the key was kept", "ключ остался")
}

// TLogoutTokensUntil: deleting a key does not revoke the tokens it was
// exchanged for — a running evo mcp keeps working until then.
func TLogoutTokensUntil(t time.Time) string {
	return i18n.Tf("tokens this key was exchanged for earlier (a running evo mcp, say) work until %s at the latest",
		"токены, выданные по этому ключу раньше (например, запущенному evo mcp), действуют не дольше чем до %s", t.Local().Format("15:04"))
}

// TLogoutRejected: IAM refused the key, so evo could neither check nor
// delete it; the profile was deleted all the same.
// It names both ids, as eKeyNotDeleted does: the console lists the key by
// its id in IAM («Риск», conclusion 42).
func TLogoutRejected(keyID, accessKeyID string) string {
	return i18n.Tf("IAM does not accept the key %s (id in IAM %s): it was revoked, expired or its secret is wrong, so evo cannot delete it. If the key is still listed in the Cloud.ru console (“Service accounts” → the account → “Access keys”), delete it there",
		"IAM не принимает ключ %s (id в IAM %s): он отозван, истек или секрет неверен, поэтому evo не может его удалить. Если ключ еще виден в личном кабинете Cloud.ru («Сервисные аккаунты» → аккаунт → «Ключи доступа»), удалите его там", keyID, accessKeyID)
}

// eKeyNotDeleted names the key by its Key ID, as the preview did, and by
// its id in IAM, as the console does: the two ids went without labels
// (plan 1.8, B-5 round 3 (в)).
func eKeyNotDeleted(keyID, accessKeyID string, err error) string {
	return i18n.Tf("the access key %s (id in IAM %s) was not deleted: %v; the profile was kept",
		"ключ доступа %s (id в IAM %s) не удален: %v; профиль сохранен", keyID, accessKeyID, err)
}
func eKeyNotDeletedHint() string {
	return i18n.T("Run evo logout again. If the error persists, delete the key in the Cloud.ru console (“Service accounts” → the account → “Access keys”) and run evo configure delete <profile>.",
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
“agent”) and its validity (7, 30 or 90 days); by default, you and 30 days. The
terminal asks nothing: the choice is made on the page. Access is
granted only by the “Grant access” button on the page; there is no --yes
flag. With your rights, evo creates a service account or reuses the
existing one, grants it the role “Project administrator” until the key
expires (a longer or unbounded role the account holds stays so, and the
result names its term), creates the key and saves it to the profile. The role allows
deleting any resources of the project and managing service accounts and
keys. The previous key evo login created for this profile is deleted. Your
tokens stay in memory only; the key's secret is never shown. A refusal
names its step and HTTP status. Sign-in does not work over SSH or in a
container. To undo: evo logout.`, `Войти в Cloud.ru через браузер и выдать доступ на странице Evolution CMCP,
которую evo открывает на этом компьютере: проект, для кого ключ (вам —
аккаунт evo, текущий профиль; агенту — аккаунт evo-agent, профиль «agent»)
и срок (7, 30 или 90 дней); по умолчанию — вам и 30 дней. В терминале evo
ничего не спрашивает: выбор делается на странице. Доступ выдается только кнопкой «Выдать доступ»
на странице, флага --yes нет. С вашими правами evo создает сервисный
аккаунт или использует уже созданный, выдает ему роль «Администратор
проекта» до конца срока ключа (если у аккаунта роль уже на дольше или
бессрочно, она остается такой, срок называет итог), создает ключ и
записывает его в профиль.
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
	return i18n.T("Project ID or name: the page shows only this project, selected; the project is checked after you sign in in the browser",
		"ID или имя проекта: на странице будет только он, отмеченный; проект проверяется после входа в браузере")
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

// EProject: --project is no ID and no name of a project.
func EProject() string {
	return i18n.T("--project: not an ID or a name of a project (empty, a control character, or longer than 128 characters)",
		"--project: не ID и не имя проекта (пусто, управляющий символ или длиннее 128 символов)")
}

// EAgentProfileForMe: the profile of the agent's key would get the
// person's (C24).
func EAgentProfileForMe(profile string) string {
	return i18n.Tf("the profile “%s” is the profile of the agent's key; the key for you is not written there",
		"профиль «%s» — профиль ключа агента; ключ для вас туда не записывается", profile)
}
func EAgentProfileForMeHint() string {
	return i18n.T("For your key, pass --profile with another profile or unset CLOUDRU_PROFILE; for the agent's key, run evo login --for agent.",
		"Для своего ключа задайте --profile с другим профилем или уберите CLOUDRU_PROFILE; для ключа агента запустите evo login --for agent.")
}

// EConfigDir: the directory of the profiles is not writable; found before
// the browser, not after the key is made.
func EConfigDir(dir string, err error) string {
	return i18n.Tf("evo cannot write to the configuration directory %s (%v): the key would have nowhere to go; nothing was started",
		"evo не может писать в каталог настроек %s (%v): ключ после входа некуда записать, вход не начат", dir, err)
}
func EConfigDirHint() string {
	return i18n.T("Make the directory writable, or point CLOUDRU_CONFIG_DIR at one that is, then run evo login again.",
		"Откройте каталог на запись или укажите в CLOUDRU_CONFIG_DIR другой, затем запустите evo login снова.")
}

// TProfileHasKey and TEnvKeyWins: said before the browser (#1095 (и)).
func TProfileHasKey(profile string) string {
	return i18n.Tf("The profile “%s” already has an access key: after the sign-in the new key replaces it.",
		"В профиле «%s» уже есть ключ доступа: после входа его заменит новый.", profile)
}
func TEnvKeyWins() string {
	return i18n.T("CLOUDRU_KEY_ID and CLOUDRU_SECRET are set: they come before the profile, so after the sign-in commands go on using the key of the variables, not the new one.",
		"Заданы CLOUDRU_KEY_ID и CLOUDRU_SECRET: они важнее профиля, и после входа команды продолжат работать с ключом из переменных, а не с новым.")
}

func ETTL(ttl string) string {
	return i18n.Tf("--key-ttl %q: allowed values are 7d, 30d and 90d", "--key-ttl %q: допустимо 7d, 30d или 90d", ttl)
}
func ETTLHint() string { return tForeverInConsole() + "." }
