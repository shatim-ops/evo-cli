// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
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

// refusingToken is a key IAM refuses at the exchange, with a hint of its
// own.
type refusingToken struct{}

func (refusingToken) Token(context.Context) (auth.Token, error) {
	return auth.Token{}, clierrors.New(clierrors.CodeAuthFailed, "IAM: the access key is refused", "Issue a new key: evo login")
}

// The verdict of a refusal carries the refusal's own hint, as whoami's
// answer does — not doctor's «fix the configuration» (D94; the reason is
// held by TestDoctor_RefusedExchangeHasWhoamisReason).
func TestDoctor_RefusalKeepsWhoamisHint(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	r := rolesRegistry(t, &rolesWorld{perms: `[]`}, rolesProject, refusingToken{})
	text, _ := r.handlers["whoami"](context.Background(), map[string]any{})
	var ce *clierrors.CLIError
	if !errors.As(doctorVerdict(r.doctorChecks(context.Background(), false)), &ce) || ce.Hint == "" || !strings.Contains(text, ce.Hint) {
		t.Errorf("doctor's hint %v, whoami %s — want whoami's hint", ce, text)
	}
}

// exchangeRefused is a key with no cached token that IAM refuses at the
// exchange: every Token is AUTH_FAILED, as IAM's 401 on a deleted key.
type exchangeRefused struct{ subToken }

func (exchangeRefused) Token(context.Context) (auth.Token, error) {
	return auth.Token{}, clierrors.New(clierrors.CodeAuthFailed, "IAM: invalid key", "")
}

// Gate rc.1 of 1.8, step 6 (Grok on 37a86598, 2 of 2): the revoked key
// without a cached token — doctor answered CONFIG_INVALID, whoami
// AUTH_FAILED. The refusal came at the exchange, before readKey, and the
// verdict read it as a broken configuration. One refusal of IAM, one
// reason on both (D88, D94): with a cached token and without.
func TestDoctor_RefusedExchangeHasWhoamisReason(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	world := &rolesWorld{perms: `[]`, keys: keysOf("2026-12-05T00:00:00Z")}
	checks := rolesRegistry(t, world, rolesProject, exchangeRefused{subToken{"sa-1"}}).doctorChecks(context.Background(), false)
	var dce *clierrors.CLIError
	if !errors.As(doctorVerdict(checks), &dce) {
		t.Fatalf("doctor passed a key IAM refuses: %+v", checks)
	}
	text, isErr := rolesRegistry(t, world, rolesProject, exchangeRefused{subToken{"sa-1"}}).handlers["whoami"](context.Background(), map[string]any{})
	var w struct{ Reason string }
	if err := json.Unmarshal([]byte(text), &w); err != nil || !isErr || w.Reason != dce.Code || dce.Code != clierrors.CodeAuthFailed {
		t.Errorf("doctor's reason %s, whoami: %s — want one reason, AUTH_FAILED", dce.Code, text)
	}
}

// hintedRevoked is forgetToken whose exchange after Forget is refused with
// IAM's own hint, as auth.IAMProvider refuses a rejected key.
type hintedRevoked struct{ forgetToken }

func (t *hintedRevoked) Token(ctx context.Context) (auth.Token, error) {
	if len(t.forgotten) > 0 {
		return auth.Token{}, clierrors.New(clierrors.CodeAuthFailed, "IAM: invalid key", "the hint of IAM's refusal")
	}
	return t.subToken.Token(ctx)
}
func (t *hintedRevoked) Forget(v string) { t.forgotten = append(t.forgotten, v) }

// D94, addition 2 (Grok K2 on c1748626, 2 of 2): with the token cached and
// the key revoked, doctor's hint was RefusedKeyHint alone, whoami's the
// full hint of IAM's refusal. revokedKey keeps the refusal's own hint.
func TestDoctor_RevokedWithCacheKeepsTheRefusalsHint(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	notListed := `[{"id":"ak-0","keyId":"OTHER","expiredAt":"2026-12-01T00:00:00Z"}]`
	r := rolesRegistry(t, &rolesWorld{perms: `[]`, keys: notListed}, rolesProject, &hintedRevoked{forgetToken{subToken: subToken{"sa-1"}}})
	var ce *clierrors.CLIError
	if !errors.As(doctorVerdict(r.doctorChecks(context.Background(), false)), &ce) || ce.Hint != "the hint of IAM's refusal" {
		t.Errorf("doctor's hint %v — want the refusal's own", ce)
	}
}
