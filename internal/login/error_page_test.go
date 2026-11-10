// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// Finding 182 of 1.8, «Риск», conclusion 57: the page of an error is headed
// by its step — the sign-in, the choice, or the grant; a step not named is
// the grant's (the unknown is not shown as safe).
func TestErrorPage_HeadingByStep(t *testing.T) {
	cases := map[string]string{
		stepCallback: tSignInFailed(), stepToken: tSignInFailed(), stepIDToken: tSignInFailed(),
		stepCustomers: tSignInFailed(), stepProjects: tSignInFailed(), stepBulk: tSignInFailed(),
		stepChoice: tNotGranted(), stepEvaluate: tNotGranted(),
		stepAccount: tNotComplete(), stepPermission: tNotComplete(), stepKey: tNotComplete(),
		stepKeyCheck: tNotComplete(), stepProfile: tNotComplete(), "": tNotComplete(),
	}
	for step, want := range cases {
		err := clierrors.New(clierrors.CodeAuthFailed, "refused", "")
		var e error = err
		if step != "" {
			e = at(step, err)
		}
		d := errorPage(e).data.(resultData)
		if d.Status != want || !strings.HasPrefix(d.Title, want) {
			t.Errorf("step %q: heading %q, title %q; want %q", step, d.Status, d.Title, want)
		}
		if made := d.Note == tNothingMade(); made != (want != tNotComplete()) {
			t.Errorf("step %q: the line %q", step, d.Note)
		}
	}
	if d := errorPage(errors.New("plain")).data.(resultData); d.Status != tNotComplete() || d.Note != "" {
		t.Errorf("an error with no code: %q, %q", d.Status, d.Note)
	}
}

// writeCalls are the requests of the world that write: all but GET, the
// exchange of the code and the checks of access (POST, but reads).
func writeCalls(calls []string) []string {
	var out []string
	for _, c := range calls {
		method, path, _ := strings.Cut(c, " ")
		if method == http.MethodGet || path == loginworld.IssuerPath+"/openid/token" || strings.Contains(path, "valuate") {
			continue
		}
		out = append(out, c)
	}
	return out
}

// «Риск», conclusion 57.1: the page says «evo created nothing» only where
// the login wrote nothing — to the cloud or to the disk; and it says it on
// each failure before the account (the test is not empty).
func TestErrorPage_NothingMadeMeansNoWrite(t *testing.T) {
	cases := []struct {
		name    string
		v       loginworld.Variant
		form    func(url.Values)
		nothing bool
	}{
		{"expired code", loginworld.Variant{ExpiredCode: true}, nil, true},
		{"id token", loginworld.Variant{BadIDToken: "nonce"}, nil, true},
		{"no right", loginworld.Variant{Denied: true}, nil, true},
		{"a project not offered", loginworld.Variant{}, func(f url.Values) { f.Set("project", "p-not-offered") }, true},
		{"403 on the account", loginworld.Variant{AccountForbidden: true}, nil, false},
		{"broken at the key", loginworld.Variant{FailAt: loginworld.StepKey}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, c.v)
			b := &browser{form: c.form}
			r := login(t, w, b, Choice{}, nil, nil)
			if r.err == nil {
				t.Fatalf("the login did not fail")
			}
			says := strings.Contains(b.page, tNothingMade())
			if says != c.nothing {
				t.Fatalf("the page says «nothing made»: %v, want %v\n%s", says, c.nothing, b.page)
			}
			if !says {
				return
			}
			if wr := writeCalls(w.Calls()); len(wr) > 0 || len(r.saved) > 0 || len(w.Accounts()) > 0 || len(w.Keys()) > 0 {
				t.Errorf("the page says «nothing made», but the login wrote %v, saved %v", wr, r.saved)
			}
		})
	}
}

// Finding 24 of 1.8, «Риск», conclusion 58: the lists of a rollback read as
// words, not «[a b]», and the page says what is left and where to delete
// it — or that nothing is.
func TestErrorPage_RollbackSaysWhatIsLeft(t *testing.T) {
	broken := func(context.Context) error { return errors.New("500") }
	fine := func(context.Context) error { return nil }
	for _, c := range []struct {
		made []undo
		note string
		text []string
	}{
		{[]undo{{"account evo", fine}, {"role", broken}}, eNotCompleteHint(true, ""), []string{listed([]string{"account evo", "role"}), listed([]string{"role"})}},
		{[]undo{{"account evo", fine}}, tNothingLeft(), []string{listed(nil)}},
	} {
		s := &session{made: c.made}
		err := s.rollback(context.Background(), at(stepKey, clierrors.New(clierrors.CodeAPIError, "refused", "")))
		d := errorPage(err).data.(resultData)
		if d.Status != tNotComplete() || d.Note != c.note {
			t.Errorf("%v: heading %q, line %q; want %q", c.made, d.Status, d.Note, c.note)
		}
		if strings.ContainsAny(d.Text, "[]") {
			t.Errorf("the lists as Go prints them: %q", d.Text)
		}
		for _, want := range c.text {
			if !strings.Contains(d.Text, want) {
				t.Errorf("%q does not name %q", d.Text, want)
			}
		}
	}
}
