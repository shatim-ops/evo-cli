// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// oneProfile is a store whose every audience keeps its key in one profile
// (--profile or CLOUDRU_PROFILE named it).
type oneProfile struct{ memStore }

func (*oneProfile) Profile(Audience) string { return "work" }

// The rest of finding 90 of 1.8 (the owner's decision through the release,
// 10.11): the project is picked beforehand from the profile of the
// audience the login started with; when the person chooses the other one
// on the page, the key goes to another profile — the page says so when
// that audience is chosen (by CSS, no script). The full answer, a pick
// per audience, is A's (Choice, the form of /grant; backlog 1.9).
func TestConsent_PreselectOfAnotherProfileIsNamed(t *testing.T) {
	projects := []project{{ID: "p1", Name: "one", Allowed: true}, {ID: "p2", Name: "two", Allowed: true}}
	render := func(c Choice, st Store) string {
		s := &session{cfg: Config{Now: time.Now}, choice: c, store: st, projects: projects}
		w := httptest.NewRecorder()
		writePage(w, 200, consentPage(s))
		return w.Body.String()
	}
	fromDefault := Choice{Preselect: "p1", PreselectFrom: TFromProfile("default")}
	fromAgent := Choice{For: ForAgent, Preselect: "p1", PreselectFrom: TFromProfile("agent")}
	cases := []struct {
		name string
		c    Choice
		st   Store
		// row is the plan's line of the other audience, line its text; ""
		// for none.
		row, line string
	}{
		{"me, the agent chosen on the page", fromDefault, &memStore{}, `<p class="row xr warning s-a-agent">`, tPreselectOtherProfile("default", "agent")},
		{"agent, me chosen on the page", fromAgent, &memStore{}, `<p class="row xr warning s-a-me">`, tPreselectOtherProfile("agent", "default")},
		{"one profile for both", fromDefault, &oneProfile{}, "", ""},
		{"CLOUDRU_PROJECT_ID is for both", Choice{Preselect: "p1", PreselectFrom: TFromEnv(), PreselectEnv: true}, &memStore{}, "", ""},
		{"nothing picked beforehand", Choice{}, &memStore{}, "", ""},
	}
	for _, c := range cases {
		page := render(c.c, c.st)
		if c.row != "" && !(strings.Contains(page, c.row) && strings.Contains(visibleText(page), c.line)) {
			t.Errorf("%s: no visible plan line %s %q", c.name, c.row, c.line)
		}
		if c.row == "" && strings.Contains(page, `class="row xr warning`) {
			t.Errorf("%s: a line where the profile is the same", c.name)
		}
	}
}
