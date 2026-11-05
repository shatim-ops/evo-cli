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
	"unicode"

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
	return head{Lang: i18n.Lang(), Title: title, Heading: tHeading()}
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
	// Source says where a preselected project came from (R-48).
	HTMLID, ID, Name, OrgHeading, Source string
	Allowed, Checked                     bool
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
	Account                           [3]string
	Role, Key                         [2]string
	SummaryStays, ReuseNote, Cancel   string
	SecretNote, State, Consent, For   string
	Unchecked, PreselectNote, GrantIn string
	AgentWarnTitle                    string
	AgentWarn                         [2]string
	Projects                          []consentProject
	Terms                             []consentTerm
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
	d := consentData{head: newHead(tHeading()),
		Intro: s.introLine(), ProjectLegend: tProject(), NeedAdmin: tNoRightMark(), NoRightNote: tNoRightNote(),
		ForLegend: tForWhom(), ForMe: tForMe(), ForMeNote: tForMeNote(), ForAgent: tForAgent(), ForAgentNote: tForAgentNote(),
		RoleNote: tRoleNote(), TermLegend: tTerm(), TermNote: tForeverInConsole(), SummaryHeading: tWhatWillBeMade(),
		SummaryStays: tSumStays(), ReuseNote: tSumReuseNote(), Cancel: tCancel(), SecretNote: tSecretNote(),
		State: s.state, Consent: s.consent, For: string(ForAgent),
	}
	copy(d.Account[:], cut(tSumAccountNew("\x00a", "\x00p"), "\x00a", "\x00p"))
	copy(d.Role[:], cut(tSumRole("\x00d"), "\x00d"))
	copy(d.Key[:], cut(tSumKey("\x00d"), "\x00d"))
	if s.choice.For == ForMe {
		d.For = string(ForMe)
	}
	if s.unchecked != "" {
		d.Unchecked = tUnchecked(s.unchecked)
	}
	d.GrantIn = tGrantIn()
	d.AgentWarnTitle = tAgentWarnTitle()
	copy(d.AgentWarn[:], cut(tAgentWarn("\x00d"), "\x00d"))
	var rules strings.Builder
	pre := s.preselected()
	if pre < 0 && s.choice.Preselect != "" && s.preselectDenied() {
		d.PreselectNote = tPreselectNoRight(s.choice.PreselectFrom)
	}
	checked := false
	lastOrg := ""
	for i, p := range s.projects {
		cp := consentProject{HTMLID: fmt.Sprintf("p%d", i), ID: p.ID, Name: p.Name, Allowed: p.Allowed}
		if p.Customer != lastOrg {
			cp.OrgHeading, lastOrg = p.CustomerName, p.Customer
		}
		switch {
		case i == pre:
			cp.Checked, checked = true, true
			cp.Source = tPreselectedFrom(s.choice.PreselectFrom)
		case pre < 0 && p.Allowed && !checked:
			cp.Checked, checked = true, true
		}
		d.Projects = append(d.Projects, cp)
		rules.WriteString(choiceRule(cp.HTMLID, "fieldset label[for="+cp.HTMLID+"]"))
		// R-48: the button names the chosen project.
		rules.WriteString("#" + cp.HTMLID + ":checked ~ .actions .s-" + cp.HTMLID + " { display: inline; }\n")
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
		rules.WriteString("#" + id + ":checked ~ fieldset .seg label[for=" + id + "] { color: var(--ink); border-bottom-color: var(--green); } #" +
			id + ":checked ~ .summary .s-" + id + ", #" + id + ":checked ~ .warn .s-" + id + " { display: inline; }\n")
	}
	for _, id := range []string{"a-me", "a-agent"} {
		rules.WriteString(choiceRule(id, "fieldset label[for="+id+"]"))
	}
	// The warning of a key for an agent shows when the agent is chosen.
	rules.WriteString("#a-agent:checked ~ .warn { display: flex; }\n")
	// Ids are evo's own (p0…, t-7…, a-me): no text of the cloud in the CSS.
	d.Rules = template.CSS(rules.String())
	return page{"consent", d}
}

// choiceRule is the CSS of a chosen radio: its label marked, its part of
// the summary shown.
func choiceRule(id, label string) string {
	return "#" + id + ":checked ~ " + label + " { border-left-color: var(--green); background: var(--green-bg); } #" +
		id + ":checked ~ " + label + "::before { border-color: var(--green); background: radial-gradient(circle, var(--green) 45%, var(--surface2) 50%); } #" +
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
		acc += " · " + tReused()
	}
	key := config.MaskKeyID(r.KeyID)
	if r.RevokedKeyID != "" {
		key += "; " + tRevokedPrevious(config.MaskKeyID(r.RevokedKeyID))
	}
	return page{"done", resultData{head: newHead(tDone() + " — evo"), Status: tDone(),
		Text: tDoneText(config.MaskKeyID(r.KeyID), r.Profile, r.KeyExpiresAt, r.Account == ForAgent.account()),
		Rows: []row{
			{tProject(), projectIn(r.ProjectName, r.CustomerName)},
			{upperFirst(tRowAccount()), acc},
			{upperFirst(tRowRole()), tRoleUntil(r.KeyExpiresAt)},
			{upperFirst(tRowKey()), key},
			{upperFirst(tRowProfile()), r.Profile},
		},
		Footer: tRevokeHow(),
	}}
}

// upperFirst is a row name of the terminal as the page heads it.
func upperFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
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

// errorPage names the error as the terminal does, with its code; never a
// secret: the errors of the login carry none.
func errorPage(err error) page {
	var ce *clierrors.CLIError
	text := err.Error()
	if errors.As(err, &ce) && ce.Hint != "" {
		text += ". " + ce.Hint
	}
	return page{"error", resultData{head: newHead(tNotComplete() + " — evo"), Status: tNotComplete(), Text: text,
		Footer: tDetails(clierrors.Reason(err), clierrors.ExitCode(err))}}
}
