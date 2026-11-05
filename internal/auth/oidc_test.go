// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// The access key, and later the person's code and tokens, go only to
// Cloud.ru IAM over https: one decision for mk8s's token exchange and the
// login's discovery (A0-4, release plan 1.8).
func TestTrustedOIDCURL(t *testing.T) {
	for u, want := range map[string]bool{
		"https://id.cloud.ru/auth/system/openid/token":       true,
		"https://auth.iam.cloud.ru/auth/system/openid/token": true,
		"https://ID.Cloud.RU/auth/system/openid/token":       true,
		"http://id.cloud.ru/auth/system/openid/token":        false,
		"https://id.cloud.ru:8443/auth/system/openid/token":  false,
		"https://user:pw@id.cloud.ru/auth/system":            false,
		"https://id.cloud.ru.example.com/token":              false,
		"https://example.com/id.cloud.ru":                    false,
		"https://iam.api.cloud.ru/api/v1/auth/token":         false,
		"":   false,
		"::": false,
	} {
		if got := TrustedOIDCURL(u); got != want {
			t.Errorf("TrustedOIDCURL(%q) = %v, want %v", u, got, want)
		}
	}
}

// The login table: one place for each constant of the login (A0-4); the
// token URL of mk8s is the issuer's, not a second literal.
func TestLoginTable(t *testing.T) {
	if !TrustedOIDCURL(OIDCIssuer) || !TrustedOIDCURL(OIDCDiscoveryURL) || !TrustedOIDCURL(OIDCTokenURL) {
		t.Fatalf("the table's addresses must be trusted: %s %s %s", OIDCIssuer, OIDCDiscoveryURL, OIDCTokenURL)
	}
	if OIDCDiscoveryURL != OIDCIssuer+"/.well-known/openid-configuration" || OIDCTokenURL != OIDCIssuer+"/openid/token" {
		t.Errorf("discovery %s and token %s must derive from the issuer %s", OIDCDiscoveryURL, OIDCTokenURL, OIDCIssuer)
	}
	if LoginClientID != "evo_cmcp" || LoginScope != "openid" {
		t.Errorf("client %q, scope %q", LoginClientID, LoginScope)
	}
	if len(LoginRedirectPorts) != 3 || LoginRedirectPorts[0] != 18386 || LoginRedirectPorts[1] != 23386 || LoginRedirectPorts[2] != 28386 {
		t.Errorf("redirect ports %v, want the three registered for evo_cmcp in order", LoginRedirectPorts)
	}
	if LoginRedirectURI(23386) != "http://127.0.0.1:23386/callback" {
		t.Errorf("redirect URI %q", LoginRedirectURI(23386))
	}
}

// The discovery document is taken only when its issuer is the expected
// one, byte for byte (RFC 8414 §3.3, OpenID Connect Discovery §4.3), and
// every endpoint the login uses is on a trusted host: a tampered or
// misrouted document sends neither the code nor the verifier anywhere.
func TestCheckDiscovery(t *testing.T) {
	good := Discovery{
		Issuer:                OIDCIssuer,
		AuthorizationEndpoint: OIDCIssuer + "/openid/auth",
		TokenEndpoint:         OIDCTokenURL,
		JWKSURI:               OIDCIssuer + "/openid/certs",
		CodeChallengeMethods:  []string{"plain", "S256"},
	}
	if err := CheckDiscovery(good); err != nil {
		t.Fatalf("a good document refused: %v", err)
	}
	bad := map[string]func(d *Discovery){
		"issuer with a trailing slash": func(d *Discovery) { d.Issuer = OIDCIssuer + "/" },
		"another issuer":               func(d *Discovery) { d.Issuer = "https://auth.iam.cloud.ru/auth/system" },
		"empty issuer":                 func(d *Discovery) { d.Issuer = "" },
		"authorize elsewhere":          func(d *Discovery) { d.AuthorizationEndpoint = "https://example.com/auth" },
		"token over http":              func(d *Discovery) { d.TokenEndpoint = "http://id.cloud.ru/auth/system/openid/token" },
		"jwks elsewhere":               func(d *Discovery) { d.JWKSURI = "https://example.com/certs" },
		"no endpoint":                  func(d *Discovery) { d.TokenEndpoint = "" },
		"no S256":                      func(d *Discovery) { d.CodeChallengeMethods = []string{"plain"} },
	}
	for name, spoil := range bad {
		d := good
		d.CodeChallengeMethods = append([]string(nil), good.CodeChallengeMethods...)
		spoil(&d)
		err := CheckDiscovery(d)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if ce, ok := err.(*clerrors.CLIError); !ok || ce.Code != clerrors.CodeAuthFailed {
			t.Errorf("%s: %v, want AUTH_FAILED", name, err)
		}
		if !strings.Contains(err.Error(), "discovery") {
			t.Errorf("%s: the error does not say what was refused: %v", name, err)
		}
	}
}
