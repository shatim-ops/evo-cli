// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/clock"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// A0-3 (release plan 1.8, R-26): whoami and doctor name the term of the
// key — the expiredAt of the access key of its service account whose keyId
// is the key's — and warn 5 days before it. The term is read in whoami and
// doctor only, never in every call; when IAM does not tell it (no right to
// read the keys, the key not in the list), it is unknown, without an error
// and without another exit code. The secret the list carries reaches no
// answer (R-43).

func keysOf(expiredAt string) string {
	return `[{"id":"ak-0","keyId":"OTHER","secret":"SECRET-OTHER","expiredAt":"2026-01-01T00:00:00Z"},` +
		`{"id":"ak-1","key_id":"KEYID1","secret":"SECRET-OF-THE-KEY","expired_at":"` + expiredAt + `","description":"evo"}]`
}

func at(t *testing.T, now string) {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, now)
	if err != nil {
		t.Fatal(err)
	}
	old := clock.Now
	clock.Now = func() time.Time { return tm }
	t.Cleanup(func() { clock.Now = old })
}

func TestWhoami_KeyTerm(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	for name, c := range map[string]struct {
		keys, want string
		soon       bool
	}{
		"far":  {keysOf("2026-12-05T00:00:00Z"), "2026-12-05T00:00:00Z", false},
		"soon": {keysOf("2026-11-09T00:00:00Z"), "2026-11-09T00:00:00Z", true},
	} {
		w := &rolesWorld{perms: `[]`, keys: c.keys}
		m := whoami(t, rolesRegistry(t, w, rolesProject, subToken{"sa-1"}))
		if m["key_expires_at"] != c.want || (m["key_expires_soon"] == true) != c.soon {
			t.Errorf("%s: key_expires_at %v, soon %v; want %s, %v", name, m["key_expires_at"], m["key_expires_soon"], c.want, c.soon)
		}
		if _, ok := m["key_term_note"]; ok {
			t.Errorf("%s: key_term_note with the term known: %v", name, m["key_term_note"])
		}
		if s := toJSON(m); strings.Contains(s, "SECRET") {
			t.Fatalf("%s: a key's secret in the answer: %s", name, s)
		}
		if w.keyAsks != 1 {
			t.Errorf("%s: %d requests of the keys, want 1", name, w.keyAsks)
		}
	}
}

func TestWhoami_KeyTermUnknown(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	for name, c := range map[string]struct {
		keys, note string
		tp         subToken
		asks       int
	}{
		"no right to read the keys": {"", "403", subToken{"sa-1"}, 1},
		"the key not in the list":   {`[{"id":"ak-0","keyId":"OTHER","expiredAt":"2026-12-01T00:00:00Z"}]`, "not in the list", subToken{"sa-1"}, 1},
		"a key without a term":      {`[{"id":"ak-1","keyId":"KEYID1"}]`, "no term", subToken{"sa-1"}, 1},
		"token without sub":         {keysOf("2026-12-05T00:00:00Z"), "service account", subToken{""}, 0},
	} {
		w := &rolesWorld{perms: `[]`, keys: c.keys}
		m := whoami(t, rolesRegistry(t, w, rolesProject, c.tp))
		if _, ok := m["key_expires_at"]; ok {
			t.Errorf("%s: key_expires_at %v, want none", name, m["key_expires_at"])
		}
		if note, _ := m["key_term_note"].(string); !strings.Contains(note, c.note) {
			t.Errorf("%s: key_term_note %q, want it to say %q", name, note, c.note)
		}
		if w.keyAsks != c.asks {
			t.Errorf("%s: %d requests of the keys, want %d", name, w.keyAsks, c.asks)
		}
	}
}

func TestDoctor_KeyTerm(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	check := func(keys string) (DoctorCheck, bool) {
		r := rolesRegistry(t, &rolesWorld{perms: `[]`, keys: keys}, rolesProject, subToken{"sa-1"})
		for _, c := range r.doctorChecks(t.Context(), false) {
			if c.Name == "key term" {
				return c, doctorVerdict([]DoctorCheck{c}) == nil
			}
		}
		t.Fatal("no «key term» check")
		return DoctorCheck{}, false
	}
	if c, ok := check(keysOf("2026-12-05T00:00:00Z")); c.Status != CheckOK || !ok || !strings.Contains(c.Detail, "2026-12-05") {
		t.Errorf("far: %+v", c)
	}
	c, ok := check(keysOf("2026-11-09T00:00:00Z"))
	if c.Status != CheckWarn || !ok || !strings.Contains(c.Detail, "2026-11-09") || !strings.Contains(c.Detail, "evo login") {
		t.Errorf("soon: %+v, want a warning that names the term and the way (evo login or the console)", c)
	}
	if c, ok := check(""); c.Status != CheckSkip || !ok || !strings.Contains(c.Detail, "unknown") {
		t.Errorf("unknown: %+v", c)
	}
}

// forgetToken is subToken with a cache: Forget drops the token, and the
// next Token exchanges the key again — refused when the key is revoked.
type forgetToken struct {
	subToken
	revoked   bool
	forgotten []string
}

func (t *forgetToken) Token(ctx context.Context) (auth.Token, error) {
	if len(t.forgotten) > 0 && t.revoked {
		return auth.Token{}, clierrors.New(clierrors.CodeAuthFailed, "IAM: invalid key", "")
	}
	return t.subToken.Token(ctx)
}
func (t *forgetToken) Forget(value string) { t.forgotten = append(t.forgotten, value) }

// G10, F2 (round 3 of Grok, R3-F5): the key revoked in the console, the
// cached token still worked to its deadline; whoami answered success with
// only key_term_note «not in the list». The key not in its account's list
// makes whoami drop the cached token and exchange the key again: IAM
// refuses a revoked key — AUTH_FAILED, the key named revoked, the way out;
// a key IAM still takes (the list lags) — the answer as before.
func TestWhoami_RevokedKeyFailsAndDropsTheCache(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	notListed := `[{"id":"ak-0","keyId":"OTHER","expiredAt":"2026-12-01T00:00:00Z"}]`

	tp := &forgetToken{subToken: subToken{"sa-1"}, revoked: true}
	text, isErr := rolesRegistry(t, &rolesWorld{perms: `[]`, keys: notListed}, rolesProject, tp).handlers["whoami"](context.Background(), map[string]any{})
	if !isErr || !strings.Contains(text, clierrors.CodeAuthFailed) || !strings.Contains(text, "revoked") || !strings.Contains(text, "evo login") {
		t.Errorf("a revoked key: isErr=%v, want AUTH_FAILED naming it revoked and evo login: %s", isErr, text)
	}
	if len(tp.forgotten) != 1 {
		t.Errorf("the cached token was forgotten %d times, want once", len(tp.forgotten))
	}

	tp = &forgetToken{subToken: subToken{"sa-1"}}
	m := whoami(t, rolesRegistry(t, &rolesWorld{perms: `[]`, keys: notListed}, rolesProject, tp))
	if note, _ := m["key_term_note"].(string); !strings.Contains(note, "not in the list") {
		t.Errorf("a key IAM still takes: key_term_note %q", note)
	}

	// The term known: nothing is forgotten.
	tp = &forgetToken{subToken: subToken{"sa-1"}, revoked: true}
	whoami(t, rolesRegistry(t, &rolesWorld{perms: `[]`, keys: keysOf("2026-12-05T00:00:00Z")}, rolesProject, tp))
	if len(tp.forgotten) != 0 {
		t.Errorf("a listed key: the token was forgotten")
	}
}
