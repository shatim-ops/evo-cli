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

// IsValid returns true if the token has more than minTTL remaining.
func (t Token) IsValid(minTTL time.Duration) bool {
	return time.Until(t.ExpiresAt) > minTTL
}

// TTL returns the remaining time until expiration. It goes into answers
// (whoami, doctor), so it is read on internal/clock; IsValid, a deadline,
// keeps the wall clock.
func (t Token) TTL() time.Duration {
	return t.ExpiresAt.Sub(clock.Now())
}
