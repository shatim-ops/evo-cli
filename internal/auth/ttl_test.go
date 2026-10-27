// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
)

// The TTL of the token goes into answers (whoami token_ttl_seconds, the
// TTL line of doctor), so it is read on internal/clock: two calls on a
// stopped clock answer alike, whatever second the wall clock turns between
// them (CI on Windows, 3600 and 3599; #408).
func TestTokenTTL_ReadsTheClock(t *testing.T) {
	stopped := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := clock.Now
	clock.Now = func() time.Time { return stopped }
	t.Cleanup(func() { clock.Now = old })
	tok := Token{ExpiresAt: stopped.Add(time.Hour)}
	if got := tok.TTL(); got != time.Hour {
		t.Errorf("TTL %v on a clock stopped an hour before expiry, want 1h", got)
	}
}
