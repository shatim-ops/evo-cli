// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"errors"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

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
	// A failure at a write with no rollback says nothing of what is left:
	// it is not known.
	if d := errorPage(at(stepAccount, clierrors.New(clierrors.CodeAuthFailed, "refused", ""))).data.(resultData); d.Note != "" {
		t.Errorf("no rollback: the line %q", d.Note)
	}
}
