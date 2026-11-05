// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The result says where to go back by whom the key is for: an agent goes
// on by itself, a person goes back to the terminal.
func TestDonePage_ByAudience(t *testing.T) {
	term := time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)
	agent := donePage(Result{Account: ForAgent.account(), Profile: "agent", KeyID: "k", KeyExpiresAt: term}).data.(resultData).Text
	me := donePage(Result{Account: ForMe.account(), Profile: "default", KeyID: "k", KeyExpiresAt: term}).data.(resultData).Text
	if agent != tDoneText(config.MaskKeyID("k"), "agent", term, true) || me != tDoneText(config.MaskKeyID("k"), "default", term, false) || agent == me {
		t.Errorf("agent %q, me %q", agent, me)
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
		i18n.EN: "create a service account with the required role and a key for you in the Cloud.ru personal account",
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
// not checked, and the page says why; the button names the project.
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
	checked := regexp.MustCompile(`name="project" id="p\d+" value="([^"]+)" required checked`)
	page := render("b", true)
	if m := checked.FindStringSubmatch(page); m == nil || m[1] != "b" || !strings.Contains(page, tEnvProjectPicked()) {
		t.Errorf("the variable's project is not checked with its source: %v", m)
	}
	grant := cut(tGrantInProject("\x00p"), "\x00p")
	if !strings.Contains(page, grant[0]+`<span class="pn">`) || !strings.Contains(page, `</span>`+grant[1]+`</button>`) ||
		!strings.Contains(page, `.actions .s-p1`) {
		t.Errorf("the button does not name the project")
	}
	for env, note := range map[bool]string{true: tEnvProjectNoRight(), false: tProfileProjectNoRight()} {
		page = render("c", env)
		if m := checked.FindStringSubmatch(page); m == nil || m[1] != "a" || !strings.Contains(page, note) {
			t.Errorf("a project without the right (env %v): checked %v, or no note %q", env, m, note)
		}
	}
	if page = render("", true); strings.Contains(page, tEnvProjectPicked()) {
		t.Errorf("a source without a preselection")
	}
}

// Macket v13: «You signed in as» names the e-mail or the user name, never
// sub (plan 1.8, B-3); a key for an agent shows the warning of its rights
// by CSS; the result names the organization of the project.
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
	if page := render(consentPage(s)); strings.Contains(page, "u-1") || strings.Contains(page, tSignedIn("u-1")) {
		t.Errorf("sub shown on the page")
	}
	s.who.Login = "ivan@example.com"
	page := render(consentPage(s))
	if !strings.Contains(page, tSignedIn("ivan@example.com")) {
		t.Errorf("no e-mail")
	}
	if !strings.Contains(page, tAgentWarnTitle()) || !strings.Contains(page, "#a-agent:checked ~ .warn { display: flex; }") {
		t.Errorf("no warning for an agent")
	}
	done := render(donePage(Result{Account: "evo", ProjectName: "alpha", CustomerName: "Radar", KeyID: "k", KeyExpiresAt: time.Now()}))
	if !strings.Contains(done, "alpha · Radar") {
		t.Errorf("the result does not name the organization")
	}
}

// Macket v15: the result tells in a line of its own that the account was
// there and the previous key is deleted; the pages carry the logo of
// Cloud.ru inline, nothing loaded.
func TestPages_MacketV15(t *testing.T) {
	var b strings.Builder
	term := time.Now()
	p := donePage(Result{Account: "evo-agent", AccountReused: true, RevokedKeyID: "zzzzc019", KeyID: "k", KeyExpiresAt: term})
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	if note := tDoneReused("evo-agent", term, config.MaskKeyID("zzzzc019")); !strings.Contains(b.String(), note) {
		t.Errorf("no line of the reused account and the deleted key: %q", note)
	}
	b.Reset()
	p = donePage(Result{Account: "evo-agent", KeyID: "k", KeyExpiresAt: term})
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), tDoneReused("evo-agent", term, "")) {
		t.Errorf("a new account is told as reused")
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
	if strings.Contains(b.String(), "из профиля evo") || !strings.Contains(b.String(), `value="a" required checked`) {
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
	mark := "не подтверждено платформой"
	if page := render(""); strings.Contains(page, mark) {
		t.Errorf("a confirmed right is marked")
	}
	page := render("evaluate: 500")
	if strings.Count(page, mark) != 1 {
		t.Errorf("%d marks of the rights, want 1", strings.Count(page, mark))
	}
	projects, _, _ := strings.Cut(page, tForWhom())
	if !strings.Contains(projects, tUnchecked("evaluate: 500")) || !strings.Contains(projects, `<legend>`+tProject()) {
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
	if !strings.Contains(b.String(), tRoleUntil(role)) {
		t.Errorf("the role's term is not the actual one")
	}
}
