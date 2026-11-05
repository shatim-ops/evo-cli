// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// random is n bytes of crypto/rand as base64url without padding: the
// verifier (32 bytes, 43 characters; RFC 7636 §4.1), state and nonce (≥128
// bits; contract B-1, step 2).
func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// challenge is the S256 code challenge of verifier (RFC 7636 §4.2).
func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
