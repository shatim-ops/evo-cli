// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"errors"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// checkedRadio is whether the consent page marks the radio id checked.
func checkedRadio(page, id string) bool {
	for _, tg := range pageTags(page) {
		if tg.name == "input" && tg.attr["id"] == id {
			_, ok := tg.attr["checked"]
			return ok
		}
	}
	return false
}

// Release plan 1.8, «B-5: evo login спрашивает только на странице»
// (owner's decision 06.11, #1084; acceptance B-5, pp. 3–4): the
// interactive evo login asks nothing in the terminal — not whom the key is
// for, not its term — and opens the browser at once; the choice is the
// page's, where «Мне» and 30 days are the default, and a key for an agent
// is only an explicit choice (R-47, R-54). The flags stay.
func TestLogin_TerminalAsksNothingThePageChooses(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil {
		t.Fatalf("login: %v\n%s", r.err, r.out)
	}
	// evo login has no way to ask in the terminal any more (Config.Ask is
	// gone): the questions of the former wizard are not in its output.
	for _, q := range []string{"Кому ключ", "Срок ключа, дней"} {
		if strings.Contains(r.out, q) {
			t.Errorf("the terminal asks %q: the choice is the page's alone", q)
		}
	}
	if !checkedRadio(r.b.consent, "a-me") || checkedRadio(r.b.consent, "a-agent") {
		t.Errorf("the page does not offer «Мне» chosen: a key for an agent is only an explicit choice (R-47)")
	}
	if !checkedRadio(r.b.consent, "t-30") {
		t.Errorf("the page does not offer 30 days chosen")
	}
	// The flags stay: --for agent --ttl 7d choose on the page as before.
	w = newWorld(t, loginworld.Variant{})
	r = login(t, w, &browser{}, Choice{For: ForAgent, Days: 7}, nil, nil)
	if r.err != nil {
		t.Fatalf("login with the flags: %v\n%s", r.err, r.out)
	}
	if !checkedRadio(r.b.consent, "a-agent") || !checkedRadio(r.b.consent, "t-7") {
		t.Errorf("the flags --for agent and 7 days are not the page's choice")
	}
}

// Release plan 1.8, «B-5, дефект: страница после ошибки входа выглядит
// рабочей» (#1084; acceptance B-5, C2.1): after an error, a cancel or the
// end of the wait evo closes its loopback, and the page left in the tab
// must not offer what leads to it — the owner chose another project on
// the page after the error and got ERR_CONNECTION_REFUSED. The pages of
// the end hold no form, button or link, and say where to go: back to the
// terminal, evo login again.
func TestLogin_EndPagesLeadBackToTheTerminal(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	for _, c := range []struct {
		name string
		p    page
	}{
		{"error", errorPage(at(stepAccount, clierrors.New(clierrors.CodeAPIError, "iam API 400: limit", "")))},
		{"cancelled", cancelledPage()},
		{"an error of the provider", errorPage(errors.New("provider: server_error"))},
	} {
		t.Run(c.name, func(t *testing.T) {
			var b strings.Builder
			if err := pages.ExecuteTemplate(&b, c.p.name, c.p.data); err != nil {
				t.Fatal(err)
			}
			page := b.String()
			for _, tg := range pageTags(page) {
				switch {
				case tg.name == "form" || tg.name == "button" || tg.name == "input" || tg.name == "select":
					t.Errorf("the page of the end has <%s>: the loopback is closed, an action there fails", tg.name)
				case tg.name == "a" && tg.attr["href"] != "":
					t.Errorf("the page of the end links to %q", tg.attr["href"])
				case tg.name == "meta" && strings.EqualFold(tg.attr["http-equiv"], "refresh"):
					t.Errorf("the page of the end refreshes itself: the loopback is closed")
				}
			}
			if i, j := strings.Index(page, "<style>"), strings.Index(page, "</style>"); i >= 0 && j > i {
				page = page[:i] + page[j:]
			}
			text := strings.Join(strings.Fields(tag.ReplaceAllString(page, " ")), " ")
			if !strings.Contains(text, "терминал") || !strings.Contains(text, "evo login") {
				t.Errorf("the page does not lead back: «вернитесь в терминал и запустите evo login снова» (plan 1.8); it says:\n%s", text)
			}
		})
	}
}
