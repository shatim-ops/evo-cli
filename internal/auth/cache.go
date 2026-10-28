// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// TokenCache persists IAM tokens between runs. Entries are bound to the key
// they were issued for (CacheKey: key ID and a fingerprint of the secret): a
// token of another key, or of the same key ID with another secret, is never
// returned.
type TokenCache interface {
	Load(keyID string) (Token, bool)
	Store(keyID string, tok Token) error
	// Delete removes the token of keyID; a token of another key stays.
	Delete(keyID string) error
}

// FileCache stores one token in a JSON file with mode 0600.
type FileCache struct {
	Path string
}

type cachedToken struct {
	KeyID       string    `json:"key_id"`
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Load returns the cached token for keyID. A missing, corrupt or foreign
// file is a cache miss, not an error.
func (c FileCache) Load(keyID string) (Token, bool) {
	data, err := os.ReadFile(c.Path)
	if err != nil {
		return Token{}, false
	}
	var ct cachedToken
	if json.Unmarshal(data, &ct) != nil || ct.KeyID != keyID || ct.AccessToken == "" {
		return Token{}, false
	}
	return Token{Value: ct.AccessToken, ExpiresAt: ct.ExpiresAt}, true
}

// Store writes the token, replacing the previous one.
func (c FileCache) Store(keyID string, tok Token) error {
	data, err := json.Marshal(cachedToken{KeyID: keyID, AccessToken: tok.Value, ExpiresAt: tok.ExpiresAt})
	if err != nil {
		return err
	}
	return config.WriteFilePrivate(c.Path, data)
}

// Delete removes the file when it holds the token of keyID.
func (c FileCache) Delete(keyID string) error {
	if _, ok := c.Load(keyID); !ok {
		return nil
	}
	if err := os.Remove(c.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// CacheKey binds a cached token to the whole credential, not just the key
// ID: with a wrong secret and a live cached token of the same key ID,
// whoami and doctor would otherwise report "ok" without asking IAM. The
// fingerprint is a truncated SHA-256 of key ID and secret; the cache file
// (0600) already holds the access token itself, which is worth more.
func CacheKey(c Credentials) string {
	sum := sha256.Sum256([]byte(c.KeyID + "\x00" + c.Secret))
	return c.KeyID + ":" + hex.EncodeToString(sum[:8])
}
