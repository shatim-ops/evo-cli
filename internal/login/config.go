// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package login is evo login and evo logout (release plan 1.8, B-3;
// contract B-1): the person signs in to Cloud.ru in a browser (OpenID
// Connect, PKCE, a loopback redirect), chooses the project, whom the key is
// for and its term on a consent page evo serves on the loopback, and evo,
// with the person's token, makes or reuses the service account, grants it
// the project role until the key's term, creates the key last and keeps it
// in the profile. A failure after a write undoes what this login made
// («Риск», R-44). The person's tokens stay in memory.
package login

import (
	"net/http"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// Config is where the login goes and what it may use. Production comes from
// Default; the tests point it at the world of B-2 (internal/loginworld).
type Config struct {
	// Issuer is the OpenID Connect issuer; Trusted checks every address the
	// discovery document names (auth.TrustedOIDCURL).
	Issuer  string
	Trusted func(string) bool
	// OrgBase and AdminBase are the bases of the Organization and
	// Administration APIs; KeyToken is IAM's token exchange of an access key.
	OrgBase, AdminBase, KeyToken string
	HTTP                         *http.Client
	// Ports are the loopback ports of the registered redirect URIs.
	Ports []int
	// Open opens the URL in the person's browser; an error is no failure
	// of the login: the URL is printed too.
	Open func(url string) error
	// Say tells the person, in the terminal, the address to open.
	Say func(message, url string)
	// Tell writes a line for the person in the terminal (stderr): the
	// wizard, the summary and the steps.
	Tell func(line string)
	// Wait bounds the time from the start to the person's choice.
	Wait time.Duration
	// Now is the clock of the answer's values (key's term, login date).
	Now func() time.Time
}

// Default is the production Config.
func Default(say func(message, url string), open func(string) error) Config {
	return Config{
		Issuer: auth.OIDCIssuer, Trusted: auth.TrustedOIDCURL,
		OrgBase: "https://organization.api.cloud.ru", AdminBase: "", KeyToken: auth.IAMEndpoint,
		HTTP:  &http.Client{Timeout: 30 * time.Second, Transport: httpretry.UserAgent(nil), CheckRedirect: noRedirects},
		Ports: auth.LoginRedirectPorts[:], Open: open, Say: say,
		// wall-clock: the person has five minutes to sign in and choose
		// (contract B-1, section 1).
		Wait: 5 * time.Minute,
		Now:  clock.Now,
	}
}

// noRedirects: a request with the person's code or token is never sent on
// to wherever a Location points.
func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
