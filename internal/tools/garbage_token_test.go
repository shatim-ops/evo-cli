// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
)

// garbageCache is a token provider whose cached token names no service
// account (no sub: a token file edited by hand, another tool's token)
// until it is forgotten; the key exchanged again gives a token of sa-1.
type garbageCache struct{ forgotten []string }

func (g *garbageCache) Token(ctx context.Context) (auth.Token, error) {
	if len(g.forgotten) == 0 {
		return subToken{}.Token(ctx)
	}
	return subToken{"sa-1"}.Token(ctx)
}
func (g *garbageCache) Forget(value string) { g.forgotten = append(g.forgotten, value) }

// Findings ledger 1.8, № 98 (Grok R2 N1; part of № 48; plan 1.8, «Тесты
// „Процесса“ к закрытым по коду номерам входа»): a cached token that names
// no service account — whoami answered success, roles and the key's term
// «unknown: the IAM token names no service account», and the token stayed.
// A token without sub is not the key's: whoami drops it and exchanges the
// key again, as for a revoked key (#1144); the fresh token answers.
func TestWhoami_TokenWithoutSubIsDroppedAndExchangedAgain(t *testing.T) {
	at(t, "2026-11-05T12:00:00Z")
	tp := &garbageCache{}
	m := whoami(t, rolesRegistry(t, &rolesWorld{perms: `[]`, keys: keysOf("2026-12-05T00:00:00Z")}, rolesProject, tp))
	if len(tp.forgotten) != 1 {
		t.Errorf("the token without sub was forgotten %d times, want once", len(tp.forgotten))
	}
	for _, k := range []string{"roles_note", "key_term_note"} {
		if note, _ := m[k].(string); strings.Contains(note, "names no service account") {
			t.Errorf("%s: %q — whoami answered from the token without sub, not from the key exchanged again", k, note)
		}
	}
}
