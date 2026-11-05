// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"
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
		"https://id.cloud.ru:443/auth/system/openid/token":   false,
		"https:///auth/system/openid/token":                  false,
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
	if LoginClientID != "evo_cmcp" || LoginScope != "openid" || LoginRoleName != "platform.project.admin" {
		t.Errorf("client %q, scope %q, role %q", LoginClientID, LoginScope, LoginRoleName)
	}
	if len(LoginRedirectPorts) != 3 || LoginRedirectPorts[0] != 18386 || LoginRedirectPorts[1] != 23386 || LoginRedirectPorts[2] != 28386 {
		t.Errorf("redirect ports %v, want the three registered for evo_cmcp in order", LoginRedirectPorts)
	}
}
