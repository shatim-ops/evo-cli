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

// R-48: the page offers checked the project of the profile or of
// CLOUDRU_PROJECT_ID and says where it came from; one without the right is
// not checked, and the page says why; the button names the project.
func TestConsentPage_Preselect(t *testing.T) {
	render := func(pre string) string {
		s := &session{cfg: Config{Now: time.Now}, choice: Choice{Preselect: pre, PreselectFrom: "из профиля evo «dev»"},
			projects: []project{{ID: "a", Name: "alpha", Allowed: true}, {ID: "b", Name: "beta", Allowed: true}, {ID: "c", Name: "gamma"}}}
		var b strings.Builder
		p := consentPage(s)
		if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	checked := regexp.MustCompile(`name="project" id="p\d+" value="([^"]+)" required checked`)
	page := render("b")
	if m := checked.FindStringSubmatch(page); m == nil || m[1] != "b" || !strings.Contains(page, tPreselectedFrom("из профиля evo «dev»")) {
		t.Errorf("the profile's project is not checked with its source: %v", m)
	}
	if !strings.Contains(page, tGrantIn()) || !strings.Contains(page, `.actions .s-p1`) {
		t.Errorf("the button does not name the project")
	}
	page = render("c")
	if m := checked.FindStringSubmatch(page); m == nil || m[1] != "a" || !strings.Contains(page, tPreselectNoRight("из профиля evo «dev»")) {
		t.Errorf("a project without the right: checked %v, or no note", m)
	}
	if page = render(""); strings.Contains(page, tPreselectedFrom("из профиля evo «dev»")) {
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
	note := upperFirst(tReusedNote("evo-agent", term))
	if !strings.Contains(b.String(), note) || !strings.Contains(b.String(), tRevokedPrevious(config.MaskKeyID("zzzzc019"))) {
		t.Errorf("no line of the reused account and the deleted key")
	}
	if !strings.Contains(b.String(), `<svg class="logo"`) || strings.Contains(b.String(), "<img") || strings.Contains(b.String(), "http://") && !strings.Contains(b.String(), "xmlns=\"http://www.w3.org/2000/svg\"") {
		t.Errorf("the logo is not inline")
	}
}
