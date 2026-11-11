// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// FB180-4 (full audit of 1.8; D94): the key revoked, its token cached and
// alive — whoami answered AUTH_FAILED by D88, doctor «credentials: ok» and
// exit 0; a 401 on the roles was «skip». Doctor and whoami answer one way:
// a key IAM refuses fails doctor (Auth: exit 2), as it fails whoami.
func TestDoctor_RevokedKeyFailsAsWhoami(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	notListed := `[{"id":"ak-0","keyId":"OTHER","expiredAt":"2026-12-01T00:00:00Z"}]`
	failedAuth := func(checks []DoctorCheck, word string) bool {
		for _, c := range checks {
			if c.Status == CheckFail && c.Auth && strings.Contains(c.Detail, word) {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		name    string
		world   *rolesWorld
		revoked bool
		word    string
	}{
		{"revoked, token cached", &rolesWorld{perms: `[]`, keys: notListed}, true, "revoked"},
		{"401 on the roles", &rolesWorld{status: http.StatusUnauthorized, keys: keysOf("2026-12-05T00:00:00Z")}, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			tp := &forgetToken{subToken: subToken{"sa-1"}, revoked: c.revoked}
			r := rolesRegistry(t, c.world, rolesProject, tp)
			checks := r.doctorChecks(context.Background(), false)
			var ce *clierrors.CLIError
			if !failedAuth(checks, c.word) || !errors.As(doctorVerdict(checks), &ce) || ce.Code != clierrors.CodeAuthFailed {
				t.Errorf("doctor: %+v, verdict %v — want a failed check of the key, AUTH_FAILED (exit 2), as whoami", checks, doctorVerdict(checks))
			}
			wtp := &forgetToken{subToken: subToken{"sa-1"}, revoked: c.revoked}
			if _, isErr := rolesRegistry(t, c.world, rolesProject, wtp).handlers["whoami"](context.Background(), map[string]any{}); !isErr {
				t.Errorf("whoami answered success where doctor fails")
			}
		})
	}
	// A key IAM still takes (the list lags): no failure.
	tp := &forgetToken{subToken: subToken{"sa-1"}}
	if checks := rolesRegistry(t, &rolesWorld{perms: `[]`, keys: notListed}, rolesProject, tp).doctorChecks(context.Background(), false); failedAuth(checks, "") {
		t.Errorf("a key IAM takes failed doctor: %+v", checks)
	}
}

// refusingToken is a key IAM refuses at the exchange: no token cached.
type refusingToken struct{}

func (refusingToken) Token(context.Context) (auth.Token, error) {
	return auth.Token{}, clierrors.New(clierrors.CodeAuthFailed, "IAM: the access key is refused", "Issue a new key: evo login")
}

// The other path of D94 (Grok on candidate 37a86598, К2, 2 of 2): no token
// in the cache, the key revoked — the exchange itself is refused. whoami
// answered AUTH_FAILED, doctor «credentials: fail» but CONFIG_INVALID. The
// refusal is one point in both paths: doctor answers the reason and the
// hint whoami gives.
func TestDoctor_KeyRefusedAtTheExchangeAnswersAsWhoami(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	r := rolesRegistry(t, &rolesWorld{perms: `[]`}, rolesProject, refusingToken{})
	text, isErr := r.handlers["whoami"](context.Background(), map[string]any{})
	var ce *clierrors.CLIError
	verdict := doctorVerdict(r.doctorChecks(context.Background(), false))
	if !isErr || !errors.As(verdict, &ce) || ce.Code != clierrors.CodeAuthFailed || !strings.Contains(text, ce.Hint) {
		t.Errorf("doctor %v (hint %q), whoami %s — want AUTH_FAILED with whoami's hint", verdict, func() string {
			if ce != nil {
				return ce.Hint
			}
			return ""
		}(), text)
	}
}
