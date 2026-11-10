// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/product"
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
	Lang, Title, Heading, Product string
	Rules                         template.CSS
	// Wide: the consent's two columns, one screen of a laptop (B-5).
	Wide bool
	// Note is a line by the heading: on the consent, until when the page
	// works (after the wait the loopback is closed, B-5).
	Note string
}

// newHead is the head of a page; only the consent has the heading «… requests
// access»: a page of the end has its status as the heading (B-5: with it,
// the page of an error looked like a working one).
func newHead(title string) head {
	return head{Lang: i18n.Lang(), Title: title, Product: product.PageName}
}

func writePage(w http.ResponseWriter, code int, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The pages load nothing of another origin: no script; the fonts are the
	// loopback's own (/fonts/, embedded).
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(code)
	_ = pages.ExecuteTemplate(w, p.name, p.data)
}

type consentProject struct {
	// Source says where a preselected project came from (R-48).
	HTMLID, ID, Name, OrgName, Source string
	Allowed, Checked                  bool
}

// consentOrg is a group of the list of projects: one list, grouped by the
// organization, so the organization shown is always the chosen project's
// (B-5, the owner's decision of 06.11: two lists without a script could
// show one organization and grant a project of another, R-47).
type consentOrg struct {
	Name     string
	Projects []consentProject
}

type consentTerm struct {
	Days         int
	Label, Until string
	Checked      bool
}

type consentData struct {
	head
	Intro, ProjectLegend, NeedAdmin, NoRightNote                  string
	ForLegend, ForMe, ForMeNote, ForAgent, ForAgentNote, RoleNote string
	TermLegend, TermNote, SummaryHeading                          string
	// Account and Role are the summary's lines, cut where the chosen
	// account, project and day go in.
	Account                         [3]string
	AccountToChoose                 [2]string
	Role, Key                       [2]string
	SummaryStays, Cancel            string
	SecretNote, State, Consent, For string
	Unchecked, PreselectNote        string
	// ChooseProject is the empty first option when no project is picked
	// beforehand; FromList names the project where :has() is not there to
	// name the chosen one («Риск», conclusion 34).
	ChooseProject, Grant string
	Picked               bool
	AgentWarnTitle       string
	AgentWarn, GrantIn   [2]string
	Projects             []consentProject
	Orgs                 []consentOrg
	Terms                []consentTerm
}

// introLine is «You signed in as …» with the e-mail or the user name of
// the ID token; with neither, no line: sub is not shown (plan 1.8, B-3).
func (s *session) introLine() string {
	if s.who.Login == "" || s.who.Login == s.who.Sub {
		return ""
	}
	return tSignedIn(s.who.Login)
}

// cut is a text with marks, cut at them: the page puts its choices there.
func cut(text string, marks ...string) []string {
	var out []string
	for _, m := range marks {
		before, after, _ := strings.Cut(text, m)
		out = append(out, before)
		text = after
	}
	return append(out, text)
}

func consentPage(s *session) page {
	h := newHead(tHeading())
	h.Heading, h.Wide = tHeading(), true
	d := consentData{head: h,
		Intro: s.introLine(), ProjectLegend: tProject(), NeedAdmin: tNoRightMark(), NoRightNote: tNoRightNote(),
		ForLegend: tForWhom(), ForMe: tForMe(), ForMeNote: tForMeNote(), ForAgent: tForAgent(), ForAgentNote: tForAgentNote(),
		RoleNote: tRoleNote(), TermLegend: tTerm(), TermNote: tForeverInConsole(), SummaryHeading: tWhatWillBeMade(),
		SummaryStays: tSumStaysPage(), Cancel: tCancel(), SecretNote: tSecretNote(),
		State: s.state, Consent: s.consent, For: string(ForMe),
	}
	copy(d.Account[:], cut(tSumAccountNew("\x00a", "\x00p"), "\x00a", "\x00p"))
	copy(d.Role[:], cut(tSumRole("\x00d"), "\x00d"))
	copy(d.Key[:], cut(tSumKey("\x00d"), "\x00d"))
	// «Мне» unless --for agent: a key for an agent is only an explicit
	// choice (the owner's decision of 06.11, R-47).
	if s.choice.For == ForAgent {
		d.For = string(ForAgent)
	}
	if s.unchecked != "" {
		d.Unchecked = tUnchecked()
	}
	copy(d.GrantIn[:], cut(tGrantInProject("\x00p"), "\x00p"))
	d.AgentWarnTitle = tAgentWarnTitle()
	copy(d.AgentWarn[:], cut(tAgentWarn("\x00d"), "\x00d"))
	var rules strings.Builder
	pre := s.preselected()
	if pre < 0 && s.choice.Preselect != "" && s.preselectDenied() {
		d.PreselectNote = projectNoRight(s.choice.PreselectEnv)
	}
	// Picked beforehand: the project of the profile or the variable, the
	// one --project leaves, or the only one to choose; otherwise none — the
	// person picks it («Риск», conclusion 34.3: no first project by
	// default when the folded list hides the others).
	checked, allowed := false, 0
	for _, p := range s.projects {
		if p.Allowed {
			allowed++
		}
	}
	for i, p := range s.projects {
		cp := consentProject{HTMLID: fmt.Sprintf("p%d", i), ID: p.ID, Name: p.Name, OrgName: p.CustomerName, Allowed: p.Allowed}
		if i == 0 || p.Customer != s.projects[i-1].Customer {
			d.Orgs = append(d.Orgs, consentOrg{Name: p.CustomerName})
		}
		switch {
		case i == pre:
			cp.Checked, checked = true, true
			if s.choice.PreselectEnv {
				cp.Source = tEnvProjectPicked()
			}
		case pre < 0 && (s.choice.Project != "" || allowed == 1) && p.Allowed && !checked:
			cp.Checked, checked = true, true
		}
		d.Projects = append(d.Projects, cp)
		o := &d.Orgs[len(d.Orgs)-1]
		o.Projects = append(o.Projects, cp)
		rules.WriteString(shown(cp.HTMLID))
	}
	d.Picked = checked
	d.ChooseProject, d.Grant = tChooseProject(), tGrant()
	copy(d.AccountToChoose[:], cut(tSumAccountToChoose("\x00a"), "\x00a"))
	if !s.until.IsZero() {
		d.Note = tPageUntil(s.until)
	}
	days := s.choice.Days
	if !containsInt(Terms, days) {
		days = DefaultTerm
	}
	now := s.cfg.Now()
	for _, n := range Terms {
		d.Terms = append(d.Terms, consentTerm{Days: n, Label: tDays(n),
			Until: day(now.Add(time.Duration(n) * 24 * time.Hour)), Checked: n == days})
		id := fmt.Sprintf("t-%d", n)
		rules.WriteString("#" + id + ":checked ~ .seg label[for=" + id + "] { color: var(--ink); border-bottom-color: var(--green); }\n" + shown(id))
	}
	for _, id := range []string{"a-me", "a-agent"} {
		rules.WriteString(choiceRule(id, ".pair label[for="+id+"]") + shown(id))
	}
	// The warning of a key for an agent shows when the agent is chosen.
	// The summary and the button name the project once one is chosen;
	// before, and without :has(), they say «the project you choose» and
	// «Grant access». Without :has() the warning shows always: more said,
	// never less (R-47).
	rules.WriteString(".sheet:has(#a-agent:checked) .warn { display: flex; }\n" +
		".sheet:has(#project option:checked:not(#p-none)) .s-chosen { display: inline; }\n" +
		".sheet:has(#p-none:checked) .s-any { display: inline; }\n" +
		"@supports not selector(:has(*)) { .warn { display: flex; } .x.s-any { display: inline; } }\n")
	// Ids are evo's own (p0…, t-7…, a-me): no text of the cloud in the CSS.
	d.Rules = template.CSS(rules.String())
	return page{"consent", d}
}

// choiceRule is the CSS of a chosen radio: its label marked.
func choiceRule(id, label string) string {
	return "#" + id + ":checked ~ " + label + " { border-left-color: var(--green); background: var(--green-bg); } #" +
		id + ":checked ~ " + label + "::before { border-color: var(--green); background: radial-gradient(circle, var(--green) 45%, var(--surface2) 50%); }\n"
}

// shown is the CSS that names a chosen radio or option in the summary, the
// warning, on the button (R-48) and under the list: by :has, wherever the
// choice stands in the form.
func shown(id string) string {
	return ".sheet:has(#" + id + ":checked) .s-" + id + " { display: inline; }\n"
}

type row struct{ Term, Value string }

type resultData struct {
	head
	Status, Text, Note, Footer string
	Rows                       []row
}

func donePage(r Result) page {
	// Macket v15: what the login reused or revoked is a line of its own,
	// not a remark in the rows; a new account has none.
	note := ""
	if r.AccountReused {
		prev := ""
		if r.RevokedKeyID != "" {
			prev = config.MaskKeyID(r.RevokedKeyID)
		}
		note = tDoneReused(r.Account, roleTerm(r), prev)
	}
	rows := tRowsPage()
	return page{"done", resultData{head: newHead(tDone() + " — evo"), Status: tDone(),
		Text: tDoneText(config.MaskKeyID(r.KeyID), r.Profile, r.KeyExpiresAt, r.Account == ForAgent.account()),
		Note: note,
		Rows: []row{
			{rows[0], projectIn(r.ProjectName, r.CustomerName)},
			{rows[1], r.Account},
			{rows[2], tRoleUntil(roleTerm(r))},
			{rows[3], config.MaskKeyID(r.KeyID)},
			{rows[4], r.Profile},
		},
		Footer: tRevokeHow(),
	}}
}

// roleTerm is the role's actual term, the key's when unknown.
func roleTerm(r Result) time.Time {
	if r.RoleExpiresAt.IsZero() {
		return r.KeyExpiresAt
	}
	return r.RoleExpiresAt
}

// projectIn is a project with its organization, when it is known.
func projectIn(project, org string) string {
	if org == "" {
		return project
	}
	return project + " · " + org
}

func cancelledPage() page {
	return page{"cancelled", resultData{head: newHead(tCancelledTitle() + " — evo"), Status: tCancelledTitle(), Text: tCancelled()}}
}

// errorPage names the error as the terminal does, with its code; the hint
// with its command stays the terminal's and JSON's. Never a secret: the
// errors of the login carry none.
func errorPage(err error) page {
	return page{"error", resultData{head: newHead(tNotComplete() + " — evo"), Status: tNotComplete(), Text: err.Error(),
		Footer: tDetails(clierrors.Reason(err), clierrors.ExitCode(err))}}
}
