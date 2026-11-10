// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
	"github.com/shatim-ops/evo-cli/internal/product"
)

// Register of findings 1.8, № 182 («Риск», conclusion 57): the heading of
// the error page follows the step of the error. A login that broke before
// any write says so — and only where the world shows no write was made
// (condition 1); a write step, or no step, keeps «could not grant»
// (condition 2).
func TestErrorPage_HeadingByStep(t *testing.T) {
	cases := []struct {
		name   string
		v      loginworld.Variant
		choice Choice
		status string
	}{
		{"token", loginworld.Variant{ExpiredCode: true}, Choice{}, tSignInFailed()},
		{"id_token", loginworld.Variant{BadIDToken: "nonce"}, Choice{}, tSignInFailed()},
		{"bulk-evaluate", loginworld.Variant{Denied: true}, Choice{}, tSignInFailed()},
		{"projects", loginworld.Variant{}, Choice{Project: "nosuch"}, tSignInFailed()},
		{"account", loginworld.Variant{FailAt: loginworld.StepAccount}, Choice{}, tNotComplete()},
		{"key", loginworld.Variant{FailAt: loginworld.StepKey}, Choice{}, tNotComplete()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, c.v)
			r := login(t, w, &browser{}, c.choice, nil, nil)
			if r.err == nil {
				t.Fatal("the login did not fail")
			}
			p := errorPage(r.err).data.(resultData)
			if p.Status != c.status || p.Title != pageTitle(c.status) {
				t.Errorf("heading %q, title %q; want %q", p.Status, p.Title, c.status)
			}
			nothing := c.status != tNotComplete()
			if (p.Note == tNothingMade()) != nothing {
				t.Errorf("note %q, nothing made %v", p.Note, nothing)
			}
			if !nothing {
				return
			}
			// Condition 1: the line is true — no write to the cloud, no
			// profile saved.
			for _, call := range w.Calls() {
				m, path, _ := strings.Cut(call, " ")
				if m != http.MethodGet && (strings.HasPrefix(path, "/api/v2/serviceAccounts") || strings.HasPrefix(path, "/api/v3/permissions")) {
					t.Errorf("«nothing made», but %s", call)
				}
			}
			if len(r.saved) != 0 {
				t.Errorf("«nothing made», but saved %+v", r.saved)
			}
		})
	}
	// The steps of the choice before any write: «not granted».
	for _, step := range []string{stepChoice, stepEvaluate} {
		p := errorPage(at(step, clierrors.New(clierrors.CodeUsage, "x", ""))).data.(resultData)
		if p.Status != tNotGranted() || p.Note != tNothingMade() {
			t.Errorf("%s: heading %q, note %q", step, p.Status, p.Note)
		}
	}
	// Condition 2: an error with no step is not told as safe.
	if p := errorPage(errors.New("x")).data.(resultData); p.Status != tNotComplete() || p.Note != "" {
		t.Errorf("no step: heading %q, note %q", p.Status, p.Note)
	}
}

// № 183 and № 26: every tab of the sign-in names the product the same way
// (the refused redirects of the provider: TestLogin_ForeignStateAndSecondCallback).
func TestPages_TitleNamesTheProduct(t *testing.T) {
	for _, p := range []page{cancelledPage(), errorPage(errors.New("x")), donePage(Result{Account: "evo", KeyID: "k"})} {
		if title := p.data.(resultData).Title; !strings.HasSuffix(title, " — "+product.PageName) {
			t.Errorf("%s: title %q", p.name, title)
		}
	}
}
