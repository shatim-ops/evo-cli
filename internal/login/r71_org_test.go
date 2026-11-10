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
)

// R-71 (the owner's live pass of 10.11, step 4; «Риск», conclusion 78):
// the organization of the project was named only in the open list (the
// optgroup) and in the small line under it; the closed list and the
// line at the button did not, and projects of one name (default) live in
// several organizations. Each option names its organization, and the line
// at the button names the chosen project and its organization, by the
// :has of the choice.
func TestConsent_EachOptionNamesItsOrganization(t *testing.T) {
	page, projects := manyProjectsConsent(t)
	options := regexp.MustCompile(`(?s)<option id="(p\d+)"[^>]*>(.*?)</option>`).FindAllStringSubmatch(page, -1)
	if len(options) != len(projects) {
		t.Fatalf("%d options for %d projects", len(options), len(projects))
	}
	for i, m := range options {
		text := html.UnescapeString(m[2])
		if !strings.Contains(text, projects[i].Name) || !strings.Contains(text, projects[i].CustomerName) {
			t.Errorf("option %s %q does not name its project and organization %q", m[1], text, projects[i].CustomerName)
		}
	}
	// The line at the button: one span per project, shown by the :has of
	// its option, with the project and the organization.
	actions := regexp.MustCompile(`(?s)<div class="actions">.*?</div>\s*<p class="support small chosen">(.*?)</p>`).FindStringSubmatch(page)
	if actions == nil {
		t.Fatal("no line of the chosen project and organization at the button")
	}
	for i, p := range projects {
		id := options[i][1]
		span := regexp.MustCompile(`<span class="x s-` + id + `">([^<]*)</span>`).FindStringSubmatch(actions[1])
		if span == nil || !strings.Contains(html.UnescapeString(span[1]), p.Name) || !strings.Contains(html.UnescapeString(span[1]), p.CustomerName) {
			t.Errorf("the line at the button does not name %q of %q for %s: %v", p.Name, p.CustomerName, id, span)
		}
		if !strings.Contains(page, ".sheet:has(#"+id+":checked) .s-"+id+" { display: inline; }") {
			t.Errorf("no :has rule shows %s", id)
		}
	}
}

// A callback with this login's own state but neither a code nor an error
// is this login's request, not a foreign one: the answer says so (#1173,
// R-71), not tForeign «does not belong to the current sign-in».
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
