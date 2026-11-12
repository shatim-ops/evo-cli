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

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// renderPage is a page as the loopback serves it.
func renderPage(t *testing.T, p page) string {
	t.Helper()
	var b strings.Builder
	if err := pages.ExecuteTemplate(&b, p.name, p.data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

var (
	boldTag = regexp.MustCompile(`</?b>`)
	anyTag  = regexp.MustCompile(`<[^>]*>`)
	tipSpan = regexp.MustCompile(`(?s)<span class="tip"[^>]*>.*?</span>`)
)

// flatText is the text of markup: no tags (the bold of a line inline, the
// rest a space), no style, entities read, one space.
func flatText(s string) string {
	if i, j := strings.Index(s, "<style>"), strings.Index(s, "</style>"); i >= 0 && j > i {
		s = s[:i] + s[j:]
	}
	s = anyTag.ReplaceAllString(boldTag.ReplaceAllString(s, ""), " ")
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

// visibleText is the text of a page outside its tooltips (.tip): what a
// person reads without pointing at a «?» («Риск», conclusion 100.4).
func visibleText(page string) string { return flatText(tipSpan.ReplaceAllString(page, " ")) }

// tipText is the text of the tooltips of a page.
func tipText(page string) string {
	return flatText(strings.Join(tipSpan.FindAllString(page, -1), " "))
}

// markupStore has profiles named with markup and a previous key of evo
// login in the agent's profile.
type markupStore struct{ memStore }

func (*markupStore) Profile(a Audience) string { return `<i>prof-` + string(a) + `</i>` }
func (*markupStore) Previous(a Audience) *Previous {
	if a == ForAgent {
		return &Previous{ServiceAccountID: "sa", AccessKeyID: "ak", KeyID: "abcdef0123456789"}
	}
	return nil
}

// «Риск», conclusion 100.5: the names that come from the cloud and the
// profile — the project, the organization, the profile, the person — are
// text on every page, never markup: the bold of a line is the template's.
func TestPages_ValuesFromTheCloudAreText(t *testing.T) {
	const proj, org, who = `<img src=x onerror=alert(1)>prj`, `<b>org</b>`, `<script>me</script>@example.com`
	s := &session{cfg: Config{Now: time.Now}, who: person{Sub: "u-1", Login: who}, store: &markupStore{},
		choice: Choice{For: ForAgent, Preselect: "a", PreselectFrom: TFromProfile("x")},
		projects: []project{{ID: "a", Name: proj, Customer: "c1", CustomerName: org, Allowed: true},
			{ID: "b", Name: "beta", Customer: "c2", CustomerName: "Two", Allowed: true}}}
	done := Result{Account: "evo-agent", ProjectName: proj, CustomerName: org, Profile: `<i>prof-agent</i>`,
		KeyID: "k", RevokedKeyID: "abcdef0123456789", AccountReused: true, KeyExpiresAt: time.Now()}
	for name, p := range map[string]page{
		"consent": consentPage(s),
		"done":    donePage(done).signed(who),
		"error":   errorPage(at(stepAccount, clierrors.New(clierrors.CodeAPIError, "project "+proj+" of "+org, ""))).signed(who),
		"refused": refusedPage(tForeignTitle(), tForeignLink()).signed(who),
	} {
		page := renderPage(t, p)
		for _, raw := range []string{"<img", "<script>", "<b>org</b>", "<i>prof"} {
			if strings.Contains(page, raw) {
				t.Errorf("%s: %q stands in the page as markup", name, raw)
			}
		}
		if !strings.Contains(page, `<span class="user">&lt;script&gt;me&lt;/script&gt;@example.com</span>`) {
			t.Errorf("%s: the person is not in the header as text", name)
		}
	}
	// The plan and the rows keep their bold, set by the template.
	consent := renderPage(t, consentPage(s))
	if !strings.Contains(consent, `<b>&lt;img src=x onerror=alert(1)&gt;prj</b>`) {
		t.Errorf("the plan does not name the project in bold, as text")
	}
	result := renderPage(t, donePage(done))
	for _, want := range []string{`<b>&lt;img src=x onerror=alert(1)&gt;prj</b> · &lt;b&gt;org&lt;/b&gt;`, `<b>«&lt;i&gt;prof-agent&lt;/i&gt;»</b>`} {
		if !strings.Contains(result, want) {
			t.Errorf("the result has no %q", want)
		}
	}
	// No line of a page is markup made of strings in Go: the template
	// escapes every value (template.CSS of evo's own ids aside).
	src, err := os.ReadFile("pages.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "template.HTML") {
		t.Errorf("pages.go makes template.HTML: a value of the cloud could go in as markup")
	}
}

// V3 (the owner's task of 11.11): the header names the person on every
// page once the sign-in has named them; before it, and by sub, none.
func TestPages_UserInTheHeader(t *testing.T) {
	user := `<span class="user">ivan@example.com</span>`
	s := &session{cfg: Config{Now: time.Now}, who: person{Sub: "u-1", Login: "ivan@example.com"},
		projects: []project{{ID: "a", Name: "alpha", Allowed: true}}}
	for name, p := range map[string]page{
		"consent":   consentPage(s),
		"done":      donePage(Result{Account: "evo", KeyID: "k"}).signed(s.user()),
		"cancelled": cancelledPage().signed(s.user()),
		"error":     errorPage(errors.New("x")).signed(s.user()),
	} {
		if !strings.Contains(renderPage(t, p), user) {
			t.Errorf("%s: no %s in the header", name, user)
		}
	}
	s.who.Login = "u-1"
	if page := renderPage(t, consentPage(s)); strings.Contains(page, `class="user"`) {
		t.Errorf("sub is named in the header")
	}
	if page := renderPage(t, cancelledPage().signed("")); strings.Contains(page, `class="user"`) {
		t.Errorf("a page before the sign-in names a person")
	}
}

// V3, PlanReplace: the line «the previous key … will be deleted» stands in
// the plan only for the audience whose profile holds a key of evo login,
// shown by the choice; with none, no line.
func TestConsent_ReplaceLineOnlyWhenTheKeyIsReplaced(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	ps := []project{{ID: "a", Name: "alpha", Allowed: true}}
	page := renderPage(t, consentPage(&session{cfg: Config{Now: time.Now}, store: &markupStore{}, projects: ps}))
	line := regexp.MustCompile(`<p class="row xr s-(a-\w+)">.*?<span class="rt">(.*?)</span></p>`).FindAllStringSubmatch(page, -1)
	if len(line) != 1 || line[0][1] != "a-agent" || flatText(line[0][2]) != "Прежний ключ abcd****6789 на этом компьютере будет удален" ||
		!strings.Contains(line[0][2], "<b>abcd****6789</b>") {
		t.Errorf("the replace line: %q, want one, for the agent, with the masked key in bold", line)
	}
	if !strings.Contains(pagesHTML, "#a-agent:checked ~ .cbody .xr.s-a-agent") || !strings.Contains(pagesHTML, ".row.xr { display: none; }") {
		t.Errorf("the line of one audience is not shown by its choice")
	}
	page = renderPage(t, consentPage(&session{cfg: Config{Now: time.Now}, store: &memStore{}, projects: ps}))
	if strings.Contains(page, "Прежний ключ") {
		t.Errorf("a replace line with no key in the profile")
	}
}

// V3, the owner's task of 11.11: the buttons never wrap — the consent's
// button stood on two lines in the first package.
func TestPages_ButtonsDoNotWrap(t *testing.T) {
	rule := regexp.MustCompile(`(?m)^button \{([^}]*)\}`).FindStringSubmatch(pagesHTML)
	if rule == nil || !strings.Contains(rule[1], "white-space: nowrap") || !strings.Contains(rule[1], "flex: none") {
		t.Errorf("the buttons may wrap: %q", rule)
	}
}
