// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Discovery is what the login reads of the issuer's discovery document
// (OpenID Connect Discovery 1.0): its endpoints come only from here.
type Discovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
}

// discover reads the discovery document of c.Issuer and takes it only
// when CheckDiscovery does (contract B-1, step 2; invariant 1).
func (c *Config) discover(ctx context.Context) (Discovery, error) {
	var d Discovery
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return d, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return d, clierrors.Wrap(clierrors.CodeAPIUnreachable,
			eOIDCUnreachable(),
			clierrors.NetworkHint(err, eNetworkRetry()), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &d) != nil {
		return d, refuseDiscovery(c.Issuer, fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	return d, CheckDiscovery(d, c.Issuer, c.Trusted)
}

// CheckDiscovery takes a discovery document only when its issuer is
// issuer byte for byte (RFC 8414 §3.3, OpenID Connect Discovery §4.3),
// each endpoint the login uses passes trusted and S256 is offered:
// otherwise AUTH_FAILED, and nothing is sent.
func CheckDiscovery(d Discovery, issuer string, trusted func(string) bool) error {
	if d.Issuer != issuer {
		return refuseDiscovery(issuer, fmt.Sprintf("issuer %q", d.Issuer))
	}
	for _, e := range []struct{ name, u string }{
		{"authorization_endpoint", d.AuthorizationEndpoint},
		{"token_endpoint", d.TokenEndpoint},
		{"jwks_uri", d.JWKSURI},
	} {
		if !trusted(e.u) {
			return refuseDiscovery(issuer, fmt.Sprintf("%s %q", e.name, e.u))
		}
	}
	if !slices.Contains(d.CodeChallengeMethods, "S256") {
		return refuseDiscovery(issuer, "code_challenge_methods_supported: no S256")
	}
	return nil
}

func refuseDiscovery(issuer, what string) error {
	return clierrors.New(clierrors.CodeAuthFailed,
		eDiscovery(issuer, what),
		eDiscoveryHint())
}
