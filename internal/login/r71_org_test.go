// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// R-71 (the owner's live pass of 10.11, step 4; «Риск», conclusion 78):
// the organization of the project was named only in the open list (the
// optgroup); the closed list and the button did not, and projects of one
// name (default) live in several organizations. V3 (the owner's decision
// of 11.11, «Риск», conclusion 100.1): the organization of the chosen
// project is the caption by the list, seen with the list closed; the
// project is the plan's line above the button. Each project has its
// caption and its plan line, shown by the :has of its option, and each
// option stands in the group of its organization.
func TestConsent_EachOptionNamesItsOrganization(t *testing.T) {
	page, projects := manyProjectsConsent(t)
	options := regexp.MustCompile(`(?s)<option id="(p\d+)"[^>]*>(.*?)</option>`).FindAllStringSubmatch(page, -1)
	if len(options) != len(projects) {
		t.Fatalf("%d options for %d projects", len(options), len(projects))
	}
	group := map[string]string{}
	for _, g := range regexp.MustCompile(`(?s)<optgroup label="([^"]*)">(.*?)</optgroup>`).FindAllStringSubmatch(page, -1) {
		for _, o := range regexp.MustCompile(`<option id="(p\d+)"`).FindAllStringSubmatch(g[2], -1) {
			group[o[1]] = html.UnescapeString(g[1])
		}
	}
	caption := regexp.MustCompile(`(?s)<span class="caption">(.*?)</span>\s*</div>`).FindStringSubmatch(page)
	plan := regexp.MustCompile(`(?s)<div class="plan".*?</div>`).FindString(page)
	if caption == nil || plan == "" {
		t.Fatal("no caption by the list, or no plan above the button")
	}
	for i, p := range projects {
		id := options[i][1]
		if text := html.UnescapeString(options[i][2]); !strings.Contains(text, p.Name) || group[id] != p.CustomerName {
			t.Errorf("option %s %q in the group %q: want %q of %q", id, text, group[id], p.Name, p.CustomerName)
		}
		if span := regexp.MustCompile(`<span class="x s-` + id + `">([^<]*)</span>`).FindStringSubmatch(caption[1]); span == nil || html.UnescapeString(span[1]) != p.CustomerName {
			t.Errorf("the caption does not name %q for %s: %v", p.CustomerName, id, span)
		}
		if span := regexp.MustCompile(`<span class="x s-` + id + `">[^<]*<b>([^<]*)</b></span>`).FindStringSubmatch(plan); span == nil || html.UnescapeString(span[1]) != p.Name {
			t.Errorf("the plan does not name %q for %s: %v", p.Name, id, span)
		}
		if !strings.Contains(page, ".cbody:has(#"+id+":checked) .s-"+id+" { display: inline; }") {
			t.Errorf("no :has rule shows %s", id)
		}
	}
}

// «Риск», conclusion 100.1: the project of CLOUDRU_PROJECT_ID, picked
// beforehand, has its organization in the caption with its source.
func TestConsent_EnvProjectCaptionNamesItsOrganization(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	s := &session{cfg: Config{Now: time.Now}, choice: Choice{Preselect: "b", PreselectFrom: TFromEnv(), PreselectEnv: true},
		projects: []project{{ID: "a", Name: "default", Customer: "c1", CustomerName: "Org One", Allowed: true},
			{ID: "b", Name: "default", Customer: "c2", CustomerName: "Org Two", Allowed: true}}}
	page := renderPage(t, consentPage(s))
	if want := `<span class="x s-p1">Org Two · ` + tEnvProjectPicked() + `</span>`; !strings.Contains(page, want) {
		t.Errorf("no caption %q", want)
	}
}

func TestCallback_OwnStateWithoutCodeIsNotForeign(t *testing.T) {
	l := &loopback{s: &session{state: "own"}, host: "127.0.0.1:1", done: make(chan outcome, 1)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://"+l.host+"/callback?state=own", nil)
	r.Host = l.host
	l.ServeHTTP(w, r)
	body := html.UnescapeString(w.Body.String())
	if w.Code != http.StatusForbidden || strings.Contains(body, tForeign()) || !strings.Contains(body, tNoProviderAnswer()) {
		t.Errorf("answered %d: %.300q", w.Code, body)
	}
}
