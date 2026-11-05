// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The login table: every address and constant of Cloud.ru IAM's OpenID
// Connect that evo sends a secret to — the access key of mk8s's token
// exchange, the person's code and verifier of evo login — is here and only
// here (A0-4, release plan 1.8; the contract of the login is B-1).
const (
	// OIDCIssuer is the issuer of Cloud.ru IAM's OpenID Connect: the iss of
	// its tokens and the issuer its discovery document must name.
	OIDCIssuer = "https://id.cloud.ru/auth/system"
	// OIDCDiscoveryURL is the issuer's discovery document (OpenID Connect
	// Discovery 1.0): the login takes its endpoints only from it.
	OIDCDiscoveryURL = OIDCIssuer + "/.well-known/openid-configuration"
	// OIDCTokenURL is the issuer's token endpoint, the one the mk8s
	// cloudlogin exec-plugin exchanges the access key at.
	OIDCTokenURL = OIDCIssuer + "/openid/token"

	// LoginClientID is evo's public client of the login (no secret; PKCE).
	LoginClientID = "evo_cmcp"
	// LoginScope is the one scope the login asks for.
	LoginScope = "openid"
)

// LoginRedirectPorts are the loopback ports of evo_cmcp's three registered
// redirect URIs, in the order the login tries them.
var LoginRedirectPorts = [...]int{18386, 23386, 28386}

// LoginRedirectURI is the registered redirect URI on port.
func LoginRedirectURI(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/callback", port)
}

// trustedOIDCHosts are the IAM hosts a secret may be sent to (both answer
// the client-credentials grant, checked 2026-10-14).
var trustedOIDCHosts = map[string]bool{"id.cloud.ru": true, "auth.iam.cloud.ru": true}

// TrustedOIDCURL reports whether u is https on a Cloud.ru IAM host, with
// no user info and no port: the one check before a secret goes to an
// OpenID Connect address that came from outside the table (a kubeconfig,
// a discovery document).
func TrustedOIDCURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && p.Scheme == "https" && p.User == nil && p.Port() == "" &&
		trustedOIDCHosts[strings.ToLower(p.Hostname())]
}

// Discovery is what the login reads of the issuer's discovery document.
type Discovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
}

// CheckDiscovery takes a discovery document only when its issuer is
// OIDCIssuer byte for byte (RFC 8414 §3.3, OpenID Connect Discovery §4.3),
// each endpoint the login uses is a TrustedOIDCURL and S256 is offered:
// otherwise AUTH_FAILED, and nothing is sent.
func CheckDiscovery(d Discovery) error {
	refuse := func(what string) error {
		return clerrors.New(clerrors.CodeAuthFailed,
			i18n.Tf("the OpenID Connect discovery of %s is not trusted: %s; nothing was sent",
				"discovery OpenID Connect %s не принят: %s; ничего не отправлено", OIDCIssuer, what),
			i18n.T("This is not a problem of your account: the IAM answer is not the one evo expects. Try again later; if it repeats, report it.",
				"Это не проблема вашей учётной записи: ответ IAM не тот, что ожидает evo. Повторите позже; если повторится — сообщите об этом."))
	}
	if d.Issuer != OIDCIssuer {
		return refuse(i18n.Tf("issuer %q", "issuer %q", d.Issuer))
	}
	for name, u := range map[string]string{
		"authorization_endpoint": d.AuthorizationEndpoint,
		"token_endpoint":         d.TokenEndpoint,
		"jwks_uri":               d.JWKSURI,
	} {
		if !TrustedOIDCURL(u) {
			return refuse(fmt.Sprintf("%s %q", name, u))
		}
	}
	if !slices.Contains(d.CodeChallengeMethods, "S256") {
		return refuse("code_challenge_methods_supported: no S256")
	}
	return nil
}
