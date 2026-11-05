// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The pages of the loopback (the design of 05.11, «Дизайн — окно создания
// сервисного аккаунта»): no script, nothing from another site; the theme
// follows the system's; the choice shows in the summary by CSS alone.
//
//go:embed pages.html
var pagesHTML string

var pages = template.Must(template.New("pages").Parse(pagesHTML))

// page is a template of pages.html and its data.
type page struct {
	name string
	data any
}

// head is what every page has.
type head struct {
	Lang, Title, Heading string
	Rules                template.CSS
}

func newHead(title string) head {
	return head{Lang: i18n.Lang(), Title: title, Heading: i18n.T("evo access to a project", "Доступ evo к проекту")}
}

func writePage(w http.ResponseWriter, code int, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The pages load nothing: no script, no style or image of another origin.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(code)
	_ = pages.ExecuteTemplate(w, p.name, p.data)
}

type consentProject struct {
	HTMLID, ID, Name, OrgHeading string
	Allowed, Checked             bool
}

type consentTerm struct {
	Days         int
	Label, Until string
	Checked      bool
}

type consentData struct {
	head
	Intro, ProjectLegend, NeedAdmin, NoRightNote                   string
	ForLegend, ForMe, ForMeNote, ForAgent, ForAgentNote, RoleNote  string
	TermLegend, TermNote                                           string
	SummaryHeading, SummaryAccount, SummaryInProject, SummaryRole  string
	SummaryKey, SummaryStays, ReuseNote, Cancel, Grant, SecretNote string
	State, For                                                     string
	Projects                                                       []consentProject
	Terms                                                          []consentTerm
}

// date is a day as the page names it.
func date(t time.Time) string { return t.Format("2006-01-02") }

func consentPage(s *session) page {
	d := consentData{head: newHead(i18n.T("evo access to a project", "Доступ evo к проекту")),
		Intro: i18n.Tf("You signed in as %s. evo will create a service account and an access key with which the CLI and an agent manage the cloud in the project you choose.",
			"Вы вошли как %s. evo создаст сервисный аккаунт и ключ доступа, которыми CLI и агент будут управлять облаком в выбранном проекте.", s.who.Login),
		ProjectLegend: i18n.T("Project", "Проект"),
		NeedAdmin:     i18n.T("takes the project administrator", "нужен администратор проекта"),
		NoRightNote: i18n.T("A project where you may not create a service account cannot be chosen: its administrator can grant the access — ask them to run evo login, or to create a key in the console and pass it to you.",
			"Проект, где у вас нет права создать сервисный аккаунт, выбрать нельзя: его может выдать администратор проекта — попросите его выполнить evo login или создать ключ в консоли и передать вам."),
		ForLegend: i18n.T("For whom", "Для кого"),
		ForMe:     i18n.T("For me", "Для меня"),
		ForMeNote: i18n.T("Service account evo: evo commands in the terminal on your behalf.",
			"Сервисный аккаунт evo: команды evo в терминале от вашего имени."),
		ForAgent: i18n.T("For an agent", "Для агента"),
		ForAgentNote: i18n.T("Service account evo-agent: the key of an MCP server and an agent. A person deletes resources with their own key.",
			"Сервисный аккаунт evo-agent: ключ для MCP-сервера и агента. Ресурсы удаляет человек своим ключом."),
		RoleNote: i18n.Tf("Both accounts get the role «%s»: it includes deleting resources and managing service accounts and keys. A role without deletion for an agent comes when IAM gives one.",
			"Оба аккаунта получают роль «%s»: она включает удаление ресурсов и управление сервисными аккаунтами и ключами. Роль без права удаления для агента появится, когда её даст IAM.",
			auth.LoginRoleDisplayName),
		TermLegend: i18n.T("Term of the key", "Срок ключа"),
		TermNote: i18n.T("A key without a term is created in the console only. To extend, run evo login again.",
			"Бессрочный ключ можно создать только в консоли. Продление — повторный evo login."),
		SummaryHeading:   i18n.T("What will be created", "Что будет создано"),
		SummaryAccount:   i18n.T("The service account", "Будет создан сервисный аккаунт"),
		SummaryInProject: i18n.T("in the project", "в проекте"),
		SummaryRole:      i18n.Tf("It gets the role «%s» until", "Он получит роль «%s» до", auth.LoginRoleDisplayName),
		SummaryKey:       i18n.T("The key is valid until", "Ключ действует до"),
		SummaryStays: i18n.T("The account stays after logout; the role expires with the key.",
			"Аккаунт остаётся после выхода; роль истекает вместе с ключом."),
		ReuseNote: i18n.T("If the project has an account of that name, no new one is created: a new key is issued, the role extended, and the previous evo key of this machine revoked.",
			"Если аккаунт с таким именем уже есть в проекте, новый не создаётся: будет выпущен новый ключ, роль продлена, прежний ключ evo на этой машине отозван."),
		Cancel: i18n.T("Cancel", "Отмена"),
		Grant:  i18n.T("Grant access", "Выдать доступ"),
		SecretNote: i18n.T("The key's secret is not shown: evo on this computer creates the account and the key with your rights and keeps the key in the profile.",
			"Секрет ключа не показывается: evo на этом компьютере создаёт аккаунт и ключ вашими правами и записывает ключ в профиль."),
		State: s.state, For: string(ForMe),
	}
	if s.choice.For == ForAgent {
		d.For = string(ForAgent)
	}
	var rules strings.Builder
	checked := false
	lastOrg := ""
	for i, p := range s.projects {
		cp := consentProject{HTMLID: fmt.Sprintf("p%d", i), ID: p.ID, Name: p.Name, Allowed: p.Allowed}
		if p.Customer != lastOrg {
			cp.OrgHeading, lastOrg = p.CustomerName, p.Customer
		}
		if p.Allowed && !checked {
			cp.Checked, checked = true, true
		}
		d.Projects = append(d.Projects, cp)
		rules.WriteString(choiceRule(cp.HTMLID, "fieldset label[for="+cp.HTMLID+"]"))
	}
	days := s.choice.Days
	if !containsInt(Terms, days) {
		days = DefaultTerm
	}
	now := s.cfg.Now()
	for _, n := range Terms {
		d.Terms = append(d.Terms, consentTerm{Days: n, Label: i18n.Tf("%d days", "%d дней", n),
			Until: date(now.Add(time.Duration(n) * 24 * time.Hour)), Checked: n == days})
		id := fmt.Sprintf("t-%d", n)
		rules.WriteString("#" + id + ":checked ~ fieldset .seg label[for=" + id + "] { background: var(--surface2); box-shadow: 0 0 4px rgba(0,0,0,.04), 0 2px 4px rgba(0,0,0,.04); } #" +
			id + ":checked ~ .summary .s-" + id + " { display: inline; }\n")
	}
	for _, id := range []string{"a-me", "a-agent"} {
		rules.WriteString(choiceRule(id, "fieldset label[for="+id+"]"))
	}
	// Ids are evo's own (p0…, t-7…, a-me): no text of the cloud in the CSS.
	d.Rules = template.CSS(rules.String())
	return page{"consent", d}
}

// choiceRule is the CSS of a chosen radio: its label marked, its part of
// the summary shown.
func choiceRule(id, label string) string {
	return "#" + id + ":checked ~ " + label + " { border-color: var(--primary-decor); background: var(--primary-bg); } #" +
		id + ":checked ~ " + label + "::before { border-color: var(--primary); background: radial-gradient(circle, var(--primary) 45%, var(--surface2) 50%); } #" +
		id + ":checked ~ .summary .s-" + id + " { display: inline; }\n"
}

type row struct{ Term, Value string }

type resultData struct {
	head
	Status, Text, Footer string
	Rows                 []row
}

func donePage(r Result) page {
	acc := r.Account
	if r.AccountReused {
		acc += i18n.T(" · was there, used again", " · уже был, использован повторно")
	}
	key := i18n.Tf("Key ID %s, valid until %s", "Key ID %s, действует до %s", config.MaskKeyID(r.KeyID), date(r.KeyExpiresAt))
	if r.PreviousRevoked {
		key += i18n.T("; the previous evo key of this machine is revoked", "; прежний ключ evo на этой машине отозван")
	}
	return page{"done", resultData{head: newHead(i18n.T("Done — evo access", "Готово — доступ evo")),
		Status: i18n.T("Done", "Готово"),
		Text: i18n.Tf("The key of the service account %s is kept on this machine. Go back to the terminal.",
			"Ключ сервисного аккаунта %s сохранён на этой машине. Вернитесь в терминал.", r.Account),
		Rows: []row{
			{i18n.T("Project", "Проект"), r.ProjectName},
			{i18n.T("Account", "Аккаунт"), acc},
			{i18n.T("Role and term", "Роль и срок"), auth.LoginRoleDisplayName + i18n.Tf(", until %s", ", до %s", date(r.KeyExpiresAt))},
			{i18n.T("Key", "Ключ"), key},
		},
		Footer: i18n.T("To revoke: evo logout or the console → Service accounts. This window can be closed.",
			"Отозвать: evo logout или консоль → Сервисные аккаунты. Это окно можно закрыть."),
	}}
}

func cancelledPage() page {
	return page{"cancelled", resultData{head: newHead(i18n.T("Sign-in cancelled — evo", "Вход отменён — evo")),
		Status: i18n.T("Sign-in cancelled", "Вход отменён"),
		Text: i18n.T("Nothing was created. Run evo login again when ready. This window can be closed.",
			"Ничего не создано. Повторите evo login, когда будете готовы. Это окно можно закрыть.")}}
}

// errorPage names the error as the terminal does, with its code; never a
// secret: the errors of the login carry none.
func errorPage(err error) page {
	var ce *clierrors.CLIError
	text := err.Error()
	if errors.As(err, &ce) && ce.Hint != "" {
		text += ". " + ce.Hint
	}
	return page{"error", resultData{head: newHead(i18n.T("Access not complete — evo", "Доступ не завершён — evo")),
		Status: i18n.T("Access not complete", "Доступ не завершён"),
		Text:   text,
		Footer: i18n.Tf("Details and the exit code are in the terminal (%s, %d). This window can be closed.",
			"Подробности и код выхода — в терминале (%s, %d). Это окно можно закрыть.", clierrors.Reason(err), clierrors.ExitCode(err)),
	}}
}
