// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// «Риск», conclusion 63: unauthorized_client and invalid_scope are of the
// client evo in Cloud.ru, not of the person's account — «ask your
// organization administrator» does not help there; the hint says the
// account is fine and names the support. The other codes keep theirs.
func TestProviderHint_ClientCodesNotTheAdministrator(t *testing.T) {
	prev := i18n.Lang()
	defer i18n.SetLang(prev)
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		admin := i18n.T("administrator", "администратор")
		support := i18n.T("support", "поддержк")
		for _, code := range []string{"unauthorized_client", "invalid_scope"} {
			if h := eProviderHint(code); strings.Contains(h, admin) || !strings.Contains(h, support) {
				t.Errorf("%s %s: %q", lang, code, h)
			}
		}
		for _, code := range []string{"invalid_request", "server_error", "login_required", "vendor_specific", ""} {
			if h := eProviderHint(code); !strings.Contains(h, admin) {
				t.Errorf("%s %s: the hint changed: %q", lang, code, h)
			}
		}
	}
	// The callback gives the hint of its code.
	l := &loopback{s: &session{state: "own"}, host: "127.0.0.1:1", done: make(chan outcome, 1)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://"+l.host+"/callback?state=own&error=invalid_scope", nil)
	r.Host = l.host
	l.ServeHTTP(w, r)
	var ce *clierrors.CLIError
	if o := <-l.done; !errors.As(o.err, &ce) || ce.Hint != eProviderHint("invalid_scope") {
		t.Errorf("the callback's hint: %v", o.err)
	}
}
