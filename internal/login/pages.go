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
	"github.com/shatim-ops/evo-cli/internal/product"
)

// The pages of the loopback (the design V3 of 11.11, the designers'
// package v2): no script, nothing from another site; the theme follows the
// system's; the choice shows in the plan by CSS alone.
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
	// User is the e-mail or the user name of the person signed in, in the
	// header; "" before the sign-in and when the ID token names neither
	// (sub is not shown, plan 1.8, B-3).
	User string
	// Until is, on the consent, until when the page works (after the wait
	// the loopback is closed, B-5); it stands by User.
	Until string
}

func newHead(title string) head {
	return head{Lang: i18n.Lang(), Title: title, Product: product.PageName}
}

// signed is the page with the person's name in its header.
func (p page) signed(user string) page {
	switch d := p.data.(type) {
	case resultData:
		d.User = user
		p.data = d
	case consentData:
		d.User = user
		p.data = d
	}
	return p
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

// part is a piece of a line of the plan or of the result. Both kinds are
// plain strings that html/template escapes: a name that came from the
// cloud is never markup; Bold is set in bold by the template («Риск»,
// conclusion 100.5).
type part struct {
	Text string
	Bold bool
}

func bold(s string) part  { return part{Text: s, Bold: true} }
func plain(s string) part { return part{Text: s} }

// mark is the i-th place of a value in a text: a text is made with marks
// and filled with parts, so that the template sets the names in bold.
func mark(i int) string { return "\x00" + string(rune('a'+i)) }

// fill cuts a text made with marks and puts the i-th part at mark(i),
// wherever the language put it.
func fill(text string, values ...part) []part {
	var out []part
	for {
		at, n := -1, 0
		for i := range values {
			if j := strings.Index(text, mark(i)); j >= 0 && (at < 0 || j < at) {
				at, n = j, i
			}
		}
		if at < 0 {
			break
		}
		if at > 0 {
			out = append(out, plain(text[:at]))
		}
		if values[n].Text != "" {
			out = append(out, values[n])
		}
		text = text[at+len(mark(n)):]
	}
	if text != "" {
		out = append(out, plain(text))
	}
	return out
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

// hint is a «?» with its tooltip; ID ties the two for a screen reader.
type hint struct{ ID, Text string }

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

// planRow is a line of the plan shown for one audience only (its radio's
// id): the previous key replaced, a project picked by another profile.
type planRow struct {
	For   string
	Parts []part
}

type consentData struct {
	head
	State, Consent string

	ProjectLegend, ChooseProject, NeedAdmin string
	ProjectHint                             hint
	Picked                                  bool
	Orgs                                    []consentOrg
	Projects                                []consentProject
	// FieldError: the project picked beforehand is not chosen, for want of
	// the right; FieldWarning: the right is not confirmed (U-9, R-49).
	FieldError, FieldWarning string

	ForLegend, ForMe, ForMeNote, ForAgent, ForAgentNote, For string
	AgentWarnTitle, AgentWarnText                            string

	TermLegend string
	TermHint   hint
	Terms      []consentTerm

	// PlanAccount and PlanRole are the plan's lines, cut where the
	// account, the project, the role and the day go in.
	PlanAccount               [3]string
	AccountMe, AccountAgent   string
	PlanAnyProject, RoleName  string
	PlanRole                  [3]string
	AccountHint, RoleHint     hint
	Replace, Other            []planRow
	Grant, Cancel, SecretNote string
	SecretHint                hint
}

// user is the person as the pages name them: the e-mail or the user name
// of the ID token; with neither, none — sub is not shown (plan 1.8, B-3).
func (s *session) user() string {
	if s.who.Login == s.who.Sub {
		return ""
	}
	return s.who.Login
}

func consentPage(s *session) page {
	h := newHead(tHeading())
	h.Heading, h.User = tHeading(), s.user()
	d := consentData{head: h, State: s.state, Consent: s.consent,
		ProjectLegend: tProject(), ChooseProject: tChooseProject(), NeedAdmin: tNoRightMark(),
		ProjectHint: hint{"h-project", tNoRightNote()},
		ForLegend:   tForWhom(), ForMe: tForMe(), ForMeNote: tForMeNote(), ForAgent: tForAgent(), ForAgentNote: tForAgentNote(),
		For:            string(ForMe),
		AgentWarnTitle: tAgentWarnTitle(), AgentWarnText: tAgentWarn(),
		TermLegend: tTerm(), TermHint: hint{"h-term", tForeverInConsole()},
		AccountMe: ForMe.account(), AccountAgent: ForAgent.account(),
		PlanAnyProject: tPlanAccountToChoose(), RoleName: tQuoted(auth.LoginRoleDisplayName),
		AccountHint: hint{"h-account", tAccountHint()}, RoleHint: hint{"h-role", tRoleNote()},
		Grant: tGrant(), Cancel: tCancel(), SecretNote: tSecretNote(), SecretHint: hint{"h-secret", tSecretHint()},
	}
	copy(d.PlanAccount[:], cut(tPlanAccount(mark(0), mark(1)), mark(0), mark(1)))
	copy(d.PlanRole[:], cut(tPlanRole(mark(0), mark(1)), mark(0), mark(1)))
	// «Мне» unless --for agent: a key for an agent is only an explicit
	// choice (the owner's decision of 06.11, R-47).
	if s.choice.For == ForAgent {
		d.For = string(ForAgent)
	}
	if s.unchecked != "" {
		d.FieldWarning = tUnchecked()
	}
	var rules strings.Builder
	pre := s.preselected()
	if pre < 0 && s.choice.Preselect != "" && s.preselectDenied() {
		d.FieldError = projectNoRight(s.choice.PreselectEnv)
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
	if line, id := s.otherProfile(pre); line != "" {
		d.Other = []planRow{{For: id, Parts: []part{plain(line)}}}
	}
	// The previous key of each audience's profile, deleted after the new
	// one is kept (step 18): a line only where there is one.
	if s.store != nil {
		for _, a := range []Audience{ForMe, ForAgent} {
			if prev := s.store.Previous(a); prev != nil && prev.AccessKeyID != "" {
				d.Replace = append(d.Replace, planRow{For: "a-" + string(a),
					Parts: fill(tPlanReplace(mark(0)), bold(config.MaskKeyID(prev.KeyID)))})
			}
		}
	}
	if !s.until.IsZero() {
		d.Until = tPageUntil(s.until)
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
		rules.WriteString("#" + id + ":checked ~ .cbody .s-" + id + " { display: inline; }\n" +
			"#" + id + ":checked ~ .cbody .seg label[for=" + id + "] { background: var(--bg2); color: var(--text); }\n" +
			"#" + id + ":focus-visible ~ .cbody .seg label[for=" + id + "] { outline: 2px solid var(--primary); outline-offset: 2px; }\n")
	}
	// Ids are evo's own (p0…, t-7…, a-me): no text of the cloud in the CSS.
	d.Rules = template.CSS(rules.String())
	return page{"consent", d}
}

// otherProfile is the line of a project picked beforehand by the profile
// of the audience the login started with, and the id of the other
// audience, under which it shows: chosen on the page, that one keeps its
// key in another profile (the rest of finding 90 of 1.8). None when the
// project is CLOUDRU_PROJECT_ID's, for both, or one profile keeps both.
func (s *session) otherProfile(pre int) (line, id string) {
	if pre < 0 || s.choice.PreselectEnv || s.store == nil {
		return "", ""
	}
	from, other := ForMe, ForAgent
	if s.choice.For == ForAgent {
		from, other = ForAgent, ForMe
	}
	if a, b := s.store.Profile(from), s.store.Profile(other); a != b {
		return tPreselectOtherProfile(a, b), "a-" + string(other)
	}
	return "", ""
}

// shown is the CSS that names a chosen project in the caption of the list
// and in the plan (R-48, R-71): by :has, wherever the choice stands.
func shown(id string) string {
	return ".cbody:has(#" + id + ":checked) .s-" + id + " { display: inline; }\n"
}

// resultData is a page of the end: done, cancelled, an error, a refusal.
type resultData struct {
	head
	// Kind is ok, no or err; Icon is check, close or alarm.
	Kind, Icon string
	Status     string
	Text, Note string
	Rows       []resultRow
	// Tech is the reason, code and step of an error, as the terminal names
	// them.
	Tech, Footer string
}

// resultRow is a line of the result: Icon is user, key or lock.
type resultRow struct {
	Icon  string
	Parts []part
}

func donePage(r Result) page {
	org := ""
	if r.CustomerName != "" {
		org = " · " + r.CustomerName
	}
	account := fill(tDoneAccount(mark(0), mark(1)+mark(2), r.AccountReused), bold(r.Account), bold(r.ProjectName), plain(org))
	role := fill(tDoneRole(mark(0), tUnbounded()), bold(tQuoted(auth.LoginRoleDisplayName)))
	if !r.RoleUnbounded {
		role = fill(tDoneRole(mark(0), tUntil(mark(1))), bold(tQuoted(auth.LoginRoleDisplayName)), bold(day(roleTerm(r))))
	}
	prev := ""
	if r.RevokedKeyID != "" {
		prev = mark(3)
	}
	key := fill(tDoneKey(mark(0), mark(1), mark(2), prev), bold(config.MaskKeyID(r.KeyID)), bold(tQuoted(r.Profile)),
		bold(day(r.KeyExpiresAt)), bold(config.MaskKeyID(r.RevokedKeyID)))
	return page{"done", resultData{head: newHead(pageTitle(tDone())), Kind: "ok", Icon: "check", Status: tDone(),
		Text:   tDoneText(r.Account == ForAgent.account()),
		Rows:   []resultRow{{"user", account}, {"key", role}, {"lock", key}},
		Footer: tRevokeHow(),
	}}
}

// roleTerm is the role's actual term, the key's when unknown; zero when
// unbounded.
func roleTerm(r Result) time.Time {
	if r.RoleExpiresAt.IsZero() && !r.RoleUnbounded {
		return r.KeyExpiresAt
	}
	return r.RoleExpiresAt
}

func cancelledPage() page {
	return page{"cancelled", resultData{head: newHead(pageTitle(tCancelledTitle())), Kind: "no", Icon: "close",
		Status: tCancelledTitle(), Text: tCancelled()}}
}

// errorPage names the error as the terminal does, with its reason, code
// and step; the hint with its command stays the terminal's and JSON's, but
// for the way to delete what a rollback left. Never a secret: the errors
// of the login carry none. The heading follows the step (№ 182, «Риск»,
// conclusion 57): before any write the sign-in or the choice failed and
// nothing was made; a write step, or none named, keeps «could not grant» —
// the unknown is not told as safe.
func errorPage(err error) page {
	status, note, step := tNotComplete(), "", ""
	var ce *clierrors.CLIError
	if errors.As(err, &ce) {
		step, _ = ce.Details["step"].(string)
		switch step {
		case stepCallback, stepToken, stepIDToken, stepCustomers, stepProjects, stepBulk:
			status, note = tSignInFailed(), tNothingMade()
		case stepChoice, stepEvaluate:
			status, note = tNotGranted(), tNothingMade()
		default:
			// After a rollback: what is left and where to delete it, or
			// that nothing is (№ 24, «Риск», conclusion 58); without one,
			// nothing is said — it is not known.
			if left, ok := ce.Details["left"].([]string); ok {
				note = tNothingLeft()
				if len(left) > 0 {
					note = eNotCompleteHint(true, "")
				}
			}
		}
	}
	return page{"error", resultData{head: newHead(pageTitle(status)), Kind: "err", Icon: "alarm", Status: status,
		Text: err.Error(), Note: note, Tech: tTech(clierrors.Reason(err), clierrors.ExitCode(err), step), Footer: tRetryFooter()}}
}

// refusedPage is a refusal of the loopback that a person's tab meets: its
// text under the product's name, so that the tab is not a bare address
// (№ 26); title heads it where the refusal has one.
func refusedPage(title, text string) page {
	return page{"refused", resultData{head: newHead(product.PageName), Kind: "no", Icon: "alarm", Status: title, Text: text}}
}

// pageTitle is the tab of a page of the end: its status and the product,
// named as on the consent (№ 183).
func pageTitle(status string) string { return status + " — " + product.PageName }
