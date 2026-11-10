// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/login"
)

// RED for «1.8 — разработка (3)», backlog 1.9 (the rest of finding 90 of
// 1.8): the page picks a project beforehand by the profile of one
// audience only — the one the login started with. A person who chooses
// the other audience on the page has the project of the wrong profile
// checked (1.8 says so on the page, #1187). The full answer: the choice
// carries a project picked beforehand for each audience, and the page
// offers the chosen audience's one.
//
// The shape below — Choice.PreselectFor and loginPreselects — is a
// proposal; A decides the API (Choice, internal/cli/login.go, the form of
// /grant). It does not build until then: that is its red.
func TestLogin_PreselectForEachAudience(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-default")
	saveProfile(t, agentProfile, "p-agent")
	got := loginPreselects(loginStore{})
	if got[login.ForMe] != "p-default" || got[login.ForAgent] != "p-agent" {
		t.Errorf("picked beforehand %v: want the project of each audience's profile", got)
	}
	var c login.Choice
	c.PreselectFor = got
}
