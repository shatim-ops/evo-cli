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
	Intro, ProjectLegend, NeedAdmin, NoRightNote                  string
	ForLegend, ForMe, ForMeNote, ForAgent, ForAgentNote, RoleNote string
	TermLegend, TermNote, SummaryHeading                          string
	// Account and Role are the summary's lines, cut where the chosen
	// account, project and day go in.
	Account                                [3]string
	Role, Key                              [2]string
	SummaryStays, ReuseNote, Cancel, Grant string
	SecretNote, State, Consent, For        string
	Unchecked                              string
	Projects                               []consentProject
	Terms                                  []consentTerm
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
		Intro: tSignedIn(s.who.Login), ProjectLegend: tProject(), NeedAdmin: tNoRightMark(), NoRightNote: tNoRightNote(),
		ForLegend: tForWhom(), ForMe: tForMe(), ForMeNote: tForMeNote(), ForAgent: tForAgent(), ForAgentNote: tForAgentNote(),
		RoleNote: tRoleNote(), TermLegend: tTerm(), TermNote: tForeverInConsole(), SummaryHeading: tWhatWillBeMade(),
		SummaryStays: tSumStays(), ReuseNote: tSumReuseNote(), Cancel: tCancel(), Grant: tGrant(), SecretNote: tSecretNote(),
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
		d.Terms = append(d.Terms, consentTerm{Days: n, Label: tDays(n),
			Until: day(now.Add(time.Duration(n) * 24 * time.Hour)), Checked: n == days})
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
		acc += " · " + tReused()
	}
	key := config.MaskKeyID(r.KeyID)
	if r.RevokedKeyID != "" {
		key += "; " + tRevokedPrevious(config.MaskKeyID(r.RevokedKeyID))
	}
	return page{"done", resultData{head: newHead(tDone() + " — evo"), Status: tDone(),
		Text: tDoneText(config.MaskKeyID(r.KeyID), r.Profile, r.KeyExpiresAt),
		Rows: []row{
			{tProject(), r.ProjectName},
			{tRowAccount(), acc},
			{tRowRole(), tRoleUntil(r.KeyExpiresAt)},
			{tRowKey(), key},
			{tRowProfile(), r.Profile},
		},
		Footer: tRevokeHow(),
	}}
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
