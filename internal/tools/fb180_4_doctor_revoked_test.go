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

// refusalHint is the hint IAM's refusal of the key carries at the exchange
// (auth: the way to a new key and where the key came from).
const refusalHint = "Issue a new key: evo login. The key comes from CLOUDRU_KEY_ID / CLOUDRU_SECRET or the profile."

// revokedWithHint is a cached token whose key IAM refuses once the cache
// is dropped (revokedKey): the exchange's refusal with refusalHint.
type revokedWithHint struct {
	subToken
	forgot bool
}

func (t *revokedWithHint) Token(ctx context.Context) (auth.Token, error) {
	if t.forgot {
		return auth.Token{}, clierrors.New(clierrors.CodeAuthFailed, "IAM: invalid key", refusalHint)
	}
	return t.subToken.Token(ctx)
}
func (t *revokedWithHint) Forget(string) { t.forgot = true }

// FB180-4, addition 2 (Grok, К2 on c1748626, 2 of 2): the key revoked, its
// token cached — doctor and whoami gave AUTH_FAILED, but the hint was
// revokedKey's short one, while the same key with no cache got the
// refusal's whole hint (the way to a new key and where the key came from).
// Grok saw it as doctor against whoami: doctor ran first and dropped the
// cache. One state of the key, one hint: with the cache or without, on
// doctor and on whoami — the refusal's own.
func TestDoctor_CachedRevokedKeyHasTheRefusalsHint(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	world := &rolesWorld{perms: `[]`, keys: `[{"id":"ak-0","keyId":"OTHER","expiredAt":"2026-12-01T00:00:00Z"}]`}
	text, isErr := rolesRegistry(t, world, rolesProject, &revokedWithHint{subToken: subToken{"sa-1"}}).handlers["whoami"](context.Background(), map[string]any{})
	var w struct{ Reason, Hint string }
	if err := json.Unmarshal([]byte(text), &w); err != nil || !isErr || w.Reason != clierrors.CodeAuthFailed || !strings.Contains(w.Hint, refusalHint) {
		t.Errorf("whoami on a revoked key with its token cached: reason %s, hint %q — want AUTH_FAILED with the refusal's hint %q", w.Reason, w.Hint, refusalHint)
	}
	var ce *clierrors.CLIError
	verdict := doctorVerdict(rolesRegistry(t, world, rolesProject, &revokedWithHint{subToken: subToken{"sa-1"}}).doctorChecks(context.Background(), false))
	if !errors.As(verdict, &ce) || ce.Code != clierrors.CodeAuthFailed || ce.Hint != w.Hint {
		var hint string
		if ce != nil {
			hint = ce.Hint
		}
		t.Errorf("doctor %v, hint %q; whoami hint %q — want one hint", verdict, hint, w.Hint)
	}
}
