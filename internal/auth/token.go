// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
)

// Token represents an IAM bearer token.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

// IsValid returns true if the token has more than minTTL remaining. Every
// deadline of the token (the refresh, the pre-flight check) is decided
// here, on the wall clock: ExpiresAt carries no monotonic reading.
func (t Token) IsValid(minTTL time.Duration) bool {
	return time.Until(t.ExpiresAt) > minTTL
}

// TTL returns the remaining time until expiration: the value of answers
// (whoami, doctor, the text of the pre-flight refusal), so it is read on
// internal/clock. It decides nothing; IsValid does.
func (t Token) TTL() time.Duration {
	return t.ExpiresAt.Sub(clock.Now())
}
