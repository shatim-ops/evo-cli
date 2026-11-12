// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package login

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// V3 (release plan 1.8.2, «Вёрстка V3», invariants of «Процесс»): every
// page of evo login fits the screen of a laptop with no scroll, in both
// themes, and its buttons stand on one line. Static rules (nowrap in the
// CSS) do not hold it: what fits depends on the fonts, the longest lines
// and the states a page shows at once. Here the page is the loopback's —
// its headers and its embedded fonts — in headless Chromium, at each size
// and theme.

// viewports are the screens the pages fit with no scroll (the owner's
// task of 11.11).
var viewports = []struct{ w, h int }{{1280, 720}, {1366, 768}}

// wrappedButtons names the buttons whose text is not one line inside the
// button: its lines (the tops of its boxes) or its width past the button's.
// The height does not tell it: the CSS fixes it, and a wrapped label
// overflows instead.
const wrappedButtons = `[...document.querySelectorAll('button')].filter(b=>{` +
	`const r=document.createRange();r.selectNodeContents(b);` +
	`const tops=new Set([...r.getClientRects()].map(x=>Math.round(x.top)));` +
	`return tops.size>1||b.scrollWidth>b.clientWidth}).map(b=>b.textContent.trim()).join('; ')`

// tallestConsent is the consent with the most at once, as «разработка (4)»
// renders it for the designers' package (12.11): state 08 — five projects
// of two organizations, a previous key in both profiles, the project picked
// beforehand by the profile of the other audience (the line of finding 90),
// the sign-in's term, and «Агенту» chosen on the page; state 09 — the same
// with «the right is not confirmed» (unchecked). At 672ea7a of #1341 the
// block ends at 661 px (08) and 697 px (09) of 720.
func tallestConsent(unchecked bool) page {
	now := time.Date(2026, 11, 10, 8, 40, 0, 0, time.Local)
	s := &session{cfg: Config{Now: func() time.Time { return now }}, store: &twoProfiles{},
		who: person{Sub: "u", Login: "demo@example.com"}, until: now.Add(30 * time.Minute),
		choice: Choice{Preselect: "prj-3", PreselectFrom: TFromProfile("default")},
		projects: []project{
			{ID: "prj-1", Name: "analytics-dev", Customer: "c1", CustomerName: "ООО «Пример»", Allowed: true},
			{ID: "prj-2", Name: "analytics-prod", Customer: "c1", CustomerName: "ООО «Пример»"},
			{ID: "prj-3", Name: "ml-sandbox", Customer: "c1", CustomerName: "ООО «Пример»", Allowed: true},
			{ID: "prj-4", Name: "web-staging", Customer: "c2", CustomerName: "Demo Org", Allowed: true},
			{ID: "prj-5", Name: "web-prod", Customer: "c2", CustomerName: "Demo Org"},
		}}
	if unchecked {
		s.unchecked = "x"
	}
	p := consentPage(s)
	// «Агенту», chosen by the person on the page: the lines of that
	// audience show.
	d := p.data.(consentData)
	d.For = string(ForAgent)
	p.data = d
	return p
}

// twoProfiles keeps the person in «default» and the agent in «agent», each
// with a previous key of evo login.
type twoProfiles struct{ memStore }

func (*twoProfiles) Profile(a Audience) string {
	if a == ForAgent {
		return "agent"
	}
	return "default"
}

func (*twoProfiles) Previous(Audience) *Previous {
	return &Previous{ServiceAccountID: "sa", AccessKeyID: "ak", KeyID: "ffff00000000000000001111"}
}

func TestPages_FitTheScreenAndButtonsStayOnOneLine(t *testing.T) {
	chrome := findChrome()
	if chrome == "" {
		if runtime.GOOS == "linux" && os.Getenv("CI") != "" {
			t.Fatal("no Chromium on the Linux runner: the pages are held in a browser here (set EVO_TEST_CHROME)")
		}
		t.Skip("no Chromium: set EVO_TEST_CHROME")
	}
	prev := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(prev) })
	const user = "demo@example.com"
	done := Result{Account: "evo-agent", ProjectName: "ml-sandbox", CustomerName: "ООО «Пример»",
		Profile: "evo-agent", KeyID: "abcdef0123456789", RevokedKeyID: "0123456789abcdef", AccountReused: true,
		KeyExpiresAt: time.Now().Add(30 * 24 * time.Hour)}
	br := startChrome(t, chrome)
	for _, lang := range []string{i18n.RU, i18n.EN} {
		i18n.SetLang(lang)
		pages := map[string]page{
			"consent08": tallestConsent(false),
			"consent09": tallestConsent(true),
			"done":      donePage(done).signed(user),
			"cancelled": cancelledPage().signed(user),
			"error":     errorPage(errors.New("the cloud did not answer")).signed(user),
			"refused":   refusedPage(tForeignTitle(), tForeignLink()).signed(user),
		}
		for name, p := range pages {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveFont(w, r) {
					return
				}
				writePage(w, http.StatusOK, p)
			}))
			for _, vp := range viewports {
				for _, theme := range []string{"light", "dark"} {
					label := fmt.Sprintf("%s/%s %dx%d %s", lang, name, vp.w, vp.h, theme)
					br.call(t, br.session, "Emulation.setDeviceMetricsOverride",
						map[string]any{"width": vp.w, "height": vp.h, "deviceScaleFactor": 1, "mobile": false}, nil)
					br.call(t, br.session, "Emulation.setEmulatedMedia",
						map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}}, nil)
					br.navigate(t, srv.URL+"/")
					// The fonts are loaded before anything is measured.
					br.eval(t, `document.fonts.ready.then(()=>true)`)
					if got := br.eval(t, `(()=>{const e=document.scrollingElement;return e.scrollHeight-e.clientHeight})()`); got != "0" {
						t.Errorf("%s: scrolls by %s px", label, got)
					}
					if got := br.eval(t, wrappedButtons); got != "" {
						t.Errorf("%s: buttons not on one line: %s", label, got)
					}
				}
			}
			srv.Close()
		}
	}
}
