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

// tallestConsent is the consent with the most at once (state 08 of the
// designers' package): the agent chosen, a previous key replaced in both
// profiles, the project picked beforehand by the profile of the other
// audience (finding 90), two organizations, long names. State 09 is the
// same with «the right is not confirmed» (unchecked); it fits since
// 672ea7a of #1341 («релиз (4)», 12.11; #1343 closed).
func tallestConsent() *session {
	return &session{cfg: Config{Now: time.Now}, who: person{Sub: "u-1", Login: "ivan.ivanov@example.com"},
		store:  &twoProfiles{},
		choice: Choice{For: ForAgent, Preselect: "a", PreselectFrom: TFromProfile("evo-agent")},
		projects: []project{
			{ID: "a", Name: "Проект с длинным названием для проверки", Customer: "c1", CustomerName: "ООО «Организация с длинным именем»", Allowed: true},
			{ID: "b", Name: "beta", Customer: "c1", CustomerName: "ООО «Организация с длинным именем»", Allowed: true},
			{ID: "c", Name: "gamma", Customer: "c2", CustomerName: "Вторая организация", Allowed: false},
		}}
}

// twoProfiles keeps each audience in its own profile, each with a previous
// key of evo login.
type twoProfiles struct{ memStore }

func (*twoProfiles) Profile(a Audience) string { return "evo-" + string(a) }
func (*twoProfiles) Previous(Audience) *Previous {
	return &Previous{ServiceAccountID: "sa", AccessKeyID: "ak", KeyID: "abcdef0123456789"}
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
	s := tallestConsent()
	done := Result{Account: "evo-agent", ProjectName: s.projects[0].Name, CustomerName: s.projects[0].CustomerName,
		Profile: "evo-agent", KeyID: "abcdef0123456789", RevokedKeyID: "0123456789abcdef", AccountReused: true,
		KeyExpiresAt: time.Now().Add(30 * 24 * time.Hour)}
	br := startChrome(t, chrome)
	for _, lang := range []string{i18n.RU, i18n.EN} {
		i18n.SetLang(lang)
		unchecked := tallestConsent()
		unchecked.unchecked = "the right of the project was not read"
		pages := map[string]page{
			"consent":   consentPage(s),
			"consent09": consentPage(unchecked),
			"done":      donePage(done).signed(s.user()),
			"cancelled": cancelledPage().signed(s.user()),
			"error":     errorPage(errors.New("the cloud did not answer")).signed(s.user()),
			"refused":   refusedPage(tForeignTitle(), tForeignLink()).signed(s.user()),
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
