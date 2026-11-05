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
