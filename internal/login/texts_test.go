// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"errors"
	"html"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The result says where to go back by whom the key is for: an agent goes
// on by itself, a person goes back to the terminal.
func TestDonePage_ByAudience(t *testing.T) {
	term := time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)
	agent := donePage(Result{Account: ForAgent.account(), Profile: "agent", KeyID: "k", KeyExpiresAt: term})
	me := donePage(Result{Account: ForMe.account(), Profile: "default", KeyID: "k", KeyExpiresAt: term})
	if a, m := agent.data.(resultData).Text, me.data.(resultData).Text; a != tDoneText(true) || m != tDoneText(false) || a == m {
		t.Errorf("agent %q, me %q", a, m)
	}
	// The key, its profile and its day are the rows', by the audience.
	for p, profile := range map[*page]string{&agent: "agent", &me: "default"} {
		if got, want := flatText(renderPage(t, *p)), flatText(tDoneKey(config.MaskKeyID("k"), tQuoted(profile), day(term), "")); !strings.Contains(got, want) {
			t.Errorf("no row %q in %q", want, got)
		}
	}
}

// No right anywhere: one project is named, several are counted.
func TestNoRightIn(t *testing.T) {
	one := noRightIn([]project{{Name: "dev"}}).Error()
	many := noRightIn([]project{{Name: "dev"}, {Name: "prod"}, {Name: "test"}}).Error()
	if !strings.Contains(one, "dev") || strings.Contains(many, "dev") || !strings.Contains(many, "3") {
		t.Errorf("one %q; many %q", one, many)
	}
}

// «Риск», conclusion 28.2 on efae8e7a: a person without the right is not
// told to have the administrator run evo login — the login keeps the key
// on the computer where it runs, so on this one it is the administrator's
// key handed over. The hint names the way of the page's note.
func TestNoRightHint_NoLoginOfTheAdministrator(t *testing.T) {
	for lang, way := range map[string]string{
		i18n.EN: "create a service account with the required role and a key for you in the Cloud.ru console",
		i18n.RU: "создать для вас сервисный аккаунт с нужной ролью и ключ в личном кабинете Cloud.ru",
	} {
		prev := i18n.Lang()
		i18n.SetLang(lang)
		hint, note := eNoRightHint(), tNoRightNote()
		i18n.SetLang(prev)
		if strings.Contains(hint, "evo login") || !strings.Contains(hint, way) || !strings.Contains(note, way) {
			t.Errorf("%s: hint %q, note %q: want the way %q and no evo login", lang, hint, note, way)
		}
	}
}

// R-48: the page offers checked the project of the profile or of
// CLOUDRU_PROJECT_ID and says where it came from; one without the right is
// not checked, and the page says why; the plan above the button names the
// project (V3: the button is «Grant access», the owner's decision of 11.11).
func TestConsentPage_Preselect(t *testing.T) {
	render := func(pre string, env bool) string {
		s := &session{cfg: Config{Now: time.Now}, choice: Choice{Preselect: pre, PreselectFrom: "из CLOUDRU_PROJECT_ID", PreselectEnv: env},
			projects: []project{{ID: "a", Name: "alpha", Allowed: true}, {ID: "b", Name: "beta", Allowed: true}, {ID: "c", Name: "gamma"}}}
		var b strings.Builder
		p := consentPage(s)
		if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	checked := regexp.MustCompile(`<option id="p\d+" value="([^"]+)" selected>`)
	page := render("b", true)
	if m := checked.FindStringSubmatch(page); m == nil || m[1] != "b" || !strings.Contains(page, tEnvProjectPicked()) {
		t.Errorf("the variable's project is not checked with its source: %v", m)
	}
	plan := cut(tPlanAccount(mark(0), mark(1)), mark(0), mark(1))
	if !strings.Contains(page, `<span class="x s-p1">`+html.EscapeString(plan[1])+`<b>beta</b></span>`) ||
		!strings.Contains(page, `.cbody:has(#p1:checked) .s-p1`) {
		t.Errorf("the plan does not name the project")
	}
	for env, note := range map[bool]string{true: tEnvProjectNoRight(), false: tProfileProjectNoRight()} {
		page = render("c", env)
		// «Риск», conclusion 34.3: no other project picked in its place.
		if m := checked.FindStringSubmatch(page); m != nil || !strings.Contains(page, `<option id="p-none" value="" selected>`+tChooseProject()) || !strings.Contains(page, note) {
			t.Errorf("a project without the right (env %v): checked %v instead of none, or no note %q", env, m, note)
		}
	}
	if page = render("", true); strings.Contains(page, tEnvProjectPicked()) {
		t.Errorf("a source without a preselection")
	}
}

// Macket v13, V3: the header names the e-mail or the user name, never sub
// (plan 1.8, B-3); a key for an agent shows the warning of its rights by
// CSS; the result names the organization of the project.
func TestPages_MacketV13(t *testing.T) {
	render := func(p page) string {
		var b strings.Builder
		if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	s := &session{cfg: Config{Now: time.Now}, who: person{Sub: "u-1", Login: "u-1"},
		projects: []project{{ID: "a", Name: "alpha", Allowed: true}}}
	if page := render(consentPage(s)); strings.Contains(page, "u-1") || strings.Contains(page, `class="user"`) {
		t.Errorf("sub shown on the page")
	}
	s.who.Login = "ivan@example.com"
	page := render(consentPage(s))
	if !strings.Contains(page, `<span class="user">ivan@example.com</span>`) {
		t.Errorf("no e-mail")
	}
	if !strings.Contains(page, tAgentWarnTitle()) || !strings.Contains(pagesHTML, "#a-agent:checked ~ .cbody .alert { display: flex; }") {
		t.Errorf("no warning for an agent")
	}
	done := render(donePage(Result{Account: "evo", ProjectName: "alpha", CustomerName: "Radar", KeyID: "k", KeyExpiresAt: time.Now()}))
	if !strings.Contains(done, "<b>alpha</b> · Radar") {
		t.Errorf("the result does not name the organization")
	}
}

// Macket v15: the result tells in a line of its own that the account was
// there and the previous key is deleted; the pages carry the logo of
// Cloud.ru inline, nothing loaded.
func TestPages_MacketV15(t *testing.T) {
	var b strings.Builder
	term := time.Now()
	p := donePage(Result{Account: "evo-agent", AccountReused: true, RevokedKeyID: "zzzzc019zzzzc019", KeyID: "kkkk0000kkkk0000", KeyExpiresAt: term})
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	page := flatText(b.String())
	for _, line := range []string{tDoneAccount("evo-agent", "", true), tDoneKey(config.MaskKeyID("kkkk0000kkkk0000"), tQuoted(""), day(term), config.MaskKeyID("zzzzc019zzzzc019"))} {
		if !strings.Contains(page, flatText(line)) {
			t.Errorf("no line of the reused account and the deleted key: %q", line)
		}
	}
	b.Reset()
	p = donePage(Result{Account: "evo-agent", KeyID: "kkkk0000kkkk0000", KeyExpiresAt: term})
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	if page := flatText(b.String()); strings.Contains(page, flatText(tDoneAccount("evo-agent", "", true))) || strings.Contains(page, config.MaskKeyID("zzzzc019zzzzc019")) {
		t.Errorf("a new account is told as reused, or a key as deleted")
	}
	if !strings.Contains(b.String(), `<svg class="logo"`) || strings.Contains(b.String(), "<img") || strings.Contains(b.String(), "http://") && !strings.Contains(b.String(), "xmlns=\"http://www.w3.org/2000/svg\"") {
		t.Errorf("the logo is not inline")
	}
}

// Conclusion 27: the page names the source of a preselected project only
// for CLOUDRU_PROJECT_ID; the project of the profile is checked silently.
func TestConsentPage_PreselectSourceOnlyForTheVariable(t *testing.T) {
	s := &session{cfg: Config{Now: time.Now}, choice: Choice{Preselect: "a", PreselectFrom: "из профиля evo «dev»"},
		projects: []project{{ID: "a", Name: "alpha", Allowed: true}}}
	var b strings.Builder
	p := consentPage(s)
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "из профиля evo") || !strings.Contains(b.String(), `value="a" selected>`) {
		t.Errorf("the project of the profile: checked silently")
	}
}

// «Риск» on f8b43c29: one mark of the rights on the page — tUnchecked, by
// the list of projects, and only when the platform did not confirm the right.
func TestConsentPage_OneRightsMark(t *testing.T) {
	render := func(unchecked string) string {
		s := &session{cfg: Config{Now: time.Now}, unchecked: unchecked,
			projects: []project{{ID: "a", Name: "alpha", Allowed: true}}}
		var b strings.Builder
		p := consentPage(s)
		if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	mark := "Заранее проверить право не удалось"
	if page := render(""); strings.Contains(page, mark) {
		t.Errorf("a confirmed right is marked")
	}
	page := render("evaluate: 500")
	if strings.Count(page, mark) != 1 {
		t.Errorf("%d marks of the rights, want 1", strings.Count(page, mark))
	}
	projects, _, _ := strings.Cut(page, tForWhom())
	if !strings.Contains(projects, tUnchecked()) || !strings.Contains(projects, `<span>`+tProject()+`</span>`) {
		t.Errorf("the mark is not by the list of projects")
	}
}

// Conclusion 26: the role's actual term — a longer one the account held is
// kept and named on the result.
func TestDonePage_RoleTerm(t *testing.T) {
	key, role := time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC), time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	var b strings.Builder
	p := donePage(Result{Account: "evo", KeyID: "k", KeyExpiresAt: key, RoleExpiresAt: role})
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	roleLine := func(term string) string { return flatText(tDoneRole(tQuoted(auth.LoginRoleDisplayName), term)) }
	if !strings.Contains(flatText(b.String()), roleLine(tRoleTerm(role, false))) {
		t.Errorf("the role's term is not the actual one")
	}
	// A18-26: an unbounded role is named so, not by the key's term.
	b.Reset()
	p = donePage(Result{Account: "evo", KeyID: "k", KeyExpiresAt: key, RoleUnbounded: true, AccountReused: true})
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	page := flatText(b.String())
	if !strings.Contains(page, roleLine(tUnbounded())) || !strings.Contains(page, flatText(tDoneAccount("evo", "", true))) ||
		strings.Contains(page, roleLine(tRoleTerm(key, false))) {
		t.Errorf("an unbounded role is not named so:\n%s", b.String())
	}
}

// «Риск» and «релиз», #1029: the English texts call the Cloud.ru interface
// «Cloud.ru console», never «personal account» — also across a line break
// of a long text.
func TestEnglishTexts_CloudRuConsole(t *testing.T) {
	src, err := os.ReadFile("texts.go")
	if err != nil {
		t.Fatal(err)
	}
	if flat := strings.Join(strings.Fields(string(src)), " "); strings.Contains(flat, "personal account") {
		t.Errorf("texts.go says «personal account» in English")
	}
	prev := i18n.Lang()
	i18n.SetLang(i18n.EN)
	defer i18n.SetLang(prev)
	if long := strings.Join(strings.Fields(TLogoutLong()), " "); !strings.Contains(long, "Cloud.ru console") {
		t.Errorf("evo logout --help: %q", long)
	}
}

// Plan 1.8, «B-5, дефект CJM страницы согласия» (the owner, 06.11): the
// projects are one list grouped by the organization — two lists without a
// script could show one organization and grant a project of another
// (R-47); with nothing picked beforehand, none is.
func TestConsentPage_OneListByOrganization(t *testing.T) {
	s := &session{cfg: Config{Now: time.Now}, projects: []project{
		{ID: "a", Name: "alpha", Customer: "c1", CustomerName: "Org One", Allowed: true},
		{ID: "b", Name: "beta", Customer: "c1", CustomerName: "Org One"},
		{ID: "c", Name: "gamma", Customer: "c2", CustomerName: "Org Two", Allowed: true}}}
	var b strings.Builder
	p := consentPage(s)
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	if n := strings.Count(page, `<select name="project"`); n != 1 || strings.Contains(page, `type="radio" name="project"`) {
		t.Errorf("%d lists of projects, or a radio of a project", n)
	}
	for _, org := range []string{`<optgroup label="Org One">`, `<optgroup label="Org Two">`} {
		if strings.Count(page, org) != 1 {
			t.Errorf("no group %s", org)
		}
	}
	if !strings.Contains(page, `<option id="p1" value="b" disabled>beta — `+tNoRightMark()) {
		t.Errorf("a project without the right is not disabled with its mark")
	}
	// «Риск», conclusion 34.3: nothing picked beforehand — an empty first
	// option, and the list is required.
	if !strings.Contains(page, `<option id="p-none" value="" selected>`+tChooseProject()) || strings.Contains(page, `" selected>alpha`) {
		t.Errorf("a project is picked by default")
	}
}

// «Риск», conclusion 34.3 (06.11): with one project to choose, it is
// chosen; with two or more and nothing picked beforehand, none is.
func TestConsentPage_OneAllowedProjectIsChosen(t *testing.T) {
	render := func(ps []project) string {
		var b strings.Builder
		p := consentPage(&session{cfg: Config{Now: time.Now}, projects: ps})
		if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	one := render([]project{{ID: "a", Name: "alpha", Allowed: true}, {ID: "b", Name: "beta"}})
	if !strings.Contains(one, `<option id="p0" value="a" selected>`) || strings.Contains(one, `id="p-none"`) {
		t.Errorf("the one project to choose is not chosen")
	}
	two := render([]project{{ID: "a", Name: "alpha", Allowed: true}, {ID: "b", Name: "beta", Allowed: true}})
	if strings.Contains(two, `" selected>alpha`) || !strings.Contains(two, `id="p-none"`) {
		t.Errorf("of two projects, one is chosen by default")
	}
}

// The pages of the end have their status as the heading, not the
// consent's «… requests access» (plan 1.8, #1084).
func TestPages_EndHeadingIsTheStatus(t *testing.T) {
	for _, p := range []page{cancelledPage(), errorPage(errors.New("x")), donePage(Result{Account: "evo", KeyID: "k"})} {
		var b strings.Builder
		if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), tHeading()) || strings.Count(b.String(), "<h1") != 1 {
			t.Errorf("%s: the heading is not the page's status", p.name)
		}
	}
}

// Plan 1.8, #1084 («релиз», 06.11): before a project is chosen the
// button is «Grant access» with no project, and the plan says «in the
// project you choose»; the list is required.
func TestConsentPage_BeforeTheChoice(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	var b strings.Builder
	p := consentPage(&session{cfg: Config{Now: time.Now}, projects: []project{
		{ID: "a", Name: "alpha", Allowed: true}, {ID: "b", Name: "beta", Allowed: true}}})
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	for _, want := range []string{`<button type="submit" class="primary">Выдать доступ</button>`, `<span class="s-any"> в проекте, который вы выберете</span>`,
		`<select name="project" id="project" required`} {
		if !strings.Contains(page, want) {
			t.Errorf("no %q on the page before the choice", want)
		}
	}
	if strings.Contains(page, "из списка") {
		t.Errorf("«из списка» stands for a project")
	}
}

// G12 (round 3 of Grok, R3-F5): the line «the existing service account was
// reused» stood twice on the result page — by the heading and under the
// text: the header of every page took the page's Note as its own line
// «until when the page works». It stands once; the consent keeps its line
// in the header.
func TestDonePage_ReusedLineOnce(t *testing.T) {
	r := Result{Account: ForAgent.account(), AccountReused: true, RevokedKeyID: "f67f00000000000000000000000000007839",
		Profile: "agent", KeyID: "k", KeyExpiresAt: time.Now()}
	var b strings.Builder
	p := donePage(r)
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	page := flatText(b.String())
	line := flatText(tDoneAccount(r.Account, "", true))
	if n := strings.Count(page, line); n != 1 {
		t.Errorf("the reused line is %d times on the result page, want once", n)
	}
}
