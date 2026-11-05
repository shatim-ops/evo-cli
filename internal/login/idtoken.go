// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// person is what the ID token says of the human who signed in.
type person struct {
	Sub   string // the user's id
	Login string // e-mail, else the preferred user name, else sub
}

// verifyIDToken checks the ID token of step 7: its RS256 signature by the
// key of jwks named by kid, iss equal to issuer, aud holding clientID, the
// nonce of this login and an exp after now. Any failure is AUTH_FAILED,
// and nothing is created (contract B-1, step 7).
func (c *Config) verifyIDToken(ctx context.Context, jwks, raw, issuer, nonce string, now time.Time) (person, error) {
	fail := func(what string) (person, error) {
		return person{}, clierrors.New(clierrors.CodeAuthFailed,
			i18n.Tf("the sign-in could not be verified (ID token: %s); nothing was created",
				"не удалось проверить вход (ID token: %s); ничего не создано", what),
			i18n.T("Run 'evo login' again; if it repeats, report it.", "Повторите 'evo login'; если повторится — сообщите об этом."))
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return fail("form")
	}
	var head struct{ Alg, Kid string }
	if decodeSegment(parts[0], &head) != nil || head.Alg != "RS256" {
		return fail("alg")
	}
	pub, err := c.jwk(ctx, jwks, head.Kid)
	if err != nil {
		return fail("jwks")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err != nil || rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig) != nil {
		return fail("signature")
	}
	var claims struct {
		Iss, Sub, Nonce, Email, PreferredUsername string
		Aud                                       audience
		Exp                                       int64
	}
	if decodeSegment(parts[1], &claims) != nil {
		return fail("claims")
	}
	switch {
	case claims.Iss != issuer:
		return fail("iss")
	case !claims.Aud.has(auth.LoginClientID):
		return fail("aud")
	case claims.Nonce != nonce:
		return fail("nonce")
	case claims.Exp == 0 || !now.Before(time.Unix(claims.Exp, 0)):
		return fail("exp")
	case claims.Sub == "":
		return fail("sub")
	}
	p := person{Sub: claims.Sub, Login: claims.Email}
	if p.Login == "" {
		p.Login = claims.PreferredUsername
	}
	if p.Login == "" {
		p.Login = claims.Sub
	}
	return p, nil
}

// audience is the aud claim: one string or a list of them (RFC 7519 §4.1.3).
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a audience) has(s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

func decodeSegment(seg string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// jwk is the RSA key kid of the JWKS at jwks (RFC 7517).
func (c *Config) jwk(ctx context.Context, jwks, kid string) (*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwks, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var set struct {
		Keys []struct{ Kid, Kty, N, E string } `json:"keys"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &set) != nil {
		return nil, errors.New("jwks")
	}
	for _, k := range set.Keys {
		if k.Kid != kid || k.Kty != "RSA" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil {
			return nil, errors.New("jwk")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}, nil
	}
	return nil, errors.New("no key " + kid)
}
