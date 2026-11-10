// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// Findings ledger 1.8, № 89 (Grok R1b E1-F1; plan 1.8, «Тесты „Процесса“ к
// закрытым по коду номерам входа»): a provider error on the callback ends
// the login only with this login's state. Without the state it is anyone's
// request to 127.0.0.1 — 403, and the login waits on; with it — AUTH_FAILED
// at the step callback, nothing made. access_denied with the state is the
// cancel (TestLogin_Cancel…), not this.
func TestLogin_CallbackErrorEndsTheLoginOnlyWithItsState(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	type reply struct{ noState, withState int }
	got := make(chan reply, 1)
	var out bytes.Buffer
	cfg := worldConfig(w, &out, func(u string) error {
		au, err := url.Parse(u)
		if err != nil {
			return err
		}
		q := au.Query()
		cb, state := q.Get("redirect_uri"), q.Get("state")
		go func() {
			get := func(v url.Values) int {
				resp, err := http.Get(cb + "?" + v.Encode())
				if err != nil {
					return -1
				}
				resp.Body.Close()
				return resp.StatusCode
			}
			var r reply
			r.noState = get(url.Values{"error": {"server_error"}})
			r.withState = get(url.Values{"error": {"server_error"}, "error_description": {"boom"}, "state": {state}})
			got <- r
		}()
		return nil
	})
	_, err := Login(context.Background(), cfg, Choice{}, &memStore{})
	r := <-got
	if r.noState != http.StatusForbidden {
		t.Errorf("error= without the state: %d, want 403 and the login going on", r.noState)
	}
	if r.withState != http.StatusOK {
		t.Errorf("error= with the state: %d, want the error page (200)", r.withState)
	}
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeAuthFailed || ce.Details["step"] != stepCallback {
		t.Errorf("the login ends with %v, want AUTH_FAILED at the step %s", err, stepCallback)
	}
	if len(w.Accounts()) != 0 {
		t.Errorf("accounts made: %d, want none", len(w.Accounts()))
	}
}
