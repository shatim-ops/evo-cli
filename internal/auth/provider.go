// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import "context"

// TokenProvider abstracts credential sources.
type TokenProvider interface {
	// Token returns a valid IAM token, refreshing if needed.
	Token(ctx context.Context) (Token, error)
}
