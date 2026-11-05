// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
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
