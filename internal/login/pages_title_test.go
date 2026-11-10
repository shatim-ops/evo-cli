// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"errors"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/product"
)

var titleTag = regexp.MustCompile(`<title>([^<]*)</title>`)

// The findings 26 and 183 of 1.8: the tab of every page of the login names
// the product one way, the pages' name (D34), not «evo» on some and
// «Evolution CMCP» on others.
func TestPageTitles_NameTheProductOneWay(t *testing.T) {
	prev := i18n.Lang()
	defer i18n.SetLang(prev)
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		s := &session{cfg: Config{Now: time.Now}, choice: Choice{For: ForMe, Days: 30}}
		for name, p := range map[string]page{
			"consent":   consentPage(s),
			"done":      donePage(Result{Account: "evo-me", Profile: "default", KeyExpiresAt: time.Now()}),
			"cancelled": cancelledPage(),
			"error":     errorPage(errors.New("boom")),
		} {
			w := httptest.NewRecorder()
			writePage(w, 200, p)
			m := titleTag.FindStringSubmatch(w.Body.String())
			if m == nil {
				t.Fatalf("%s %s: no <title>", lang, name)
			}
			if title := m[1]; !strings.Contains(title, product.PageName) || strings.HasSuffix(title, "— evo") {
				t.Errorf("%s %s: title %q: want the product as %q", lang, name, title, product.PageName)
			}
		}
	}
}
