// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"net/url"
	"strings"
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
	// cloudlogin exec-plugin exchanges the access key at: written out, so
	// the contract check (scripts/api-contract-check.py) sees the request;
	// TestLoginTable holds it to the issuer.
	OIDCTokenURL = "https://id.cloud.ru/auth/system/openid/token"

	// LoginClientID is evo's public client of the login (no secret; PKCE).
	LoginClientID = "evo_cmcp"
	// LoginScope is the one scope the login asks for.
	LoginScope = "openid"

	// LoginRoleName is the product role the login grants the service
	// account on the project (roleIdentifier.roleName; the spec names it
	// in its filters). Live acceptance as roleName is a hypothesis until
	// the probe of B-5.
	LoginRoleName = "platform.project.admin"
	// LoginRoleDisplayName is how the console names LoginRoleName: the
	// consent says it to the person, no request needed.
	LoginRoleDisplayName = "Администратор проекта"

	// LoginActionCreateAccount and LoginActionGrantRole are the actions
	// of IAM evaluate asks about before any write: creating a service
	// account and granting a role in the project. Both names are in the
	// IAM action catalog (probe A1, runs 45 and 46, 2 of 2; the catalog
	// has no «create» of a service account, «edit» is the nearest). That
	// they are the actions IAM checks for these writes is a hypothesis
	// until IAM answers or the live sign-in of B-5 (an error of evaluate
	// is no refusal, «Риск» U-9).
	LoginActionCreateAccount = "iam.serviceaccount.edit"
	LoginActionGrantRole     = "iam.permission.add"
)

// LoginRedirectPorts are the loopback ports of evo_cmcp's three registered
// redirect URIs (http://127.0.0.1:<port>/callback), in the order the login
// tries them.
var LoginRedirectPorts = [...]int{18386, 23386, 28386}

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
