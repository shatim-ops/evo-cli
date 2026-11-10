// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/login"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// R-72 («Риск», conclusion 79; № 189): evo login --for agent writes the
// profile agent, while evo agent setup without --profile connects the
// client to default — silently, so the agent works with the person's key
// and evo logout --profile agent does not stop it. The login names the
// way to connect the agent to its key; setup without --profile, when the
// profile agent holds a key of evo login, says which profile it connects
// and how to connect the agent's key. The default is unchanged.
func TestLogin_ForAgentNamesTheSetupOfItsProfile(t *testing.T) {
	setupCLI(t)
	grantingWorld(t, loginworld.Variant{}, login.ForAgent)
	code, stdout, stderr := runCLI(t, "", "login", "--for", "agent", "--key-ttl", "7d")
	if code != 0 {
		t.Fatalf("login: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "evo agent setup") || !strings.Contains(stdout+stderr, "--profile agent") {
		t.Errorf("no way to connect the agent to its key:\n%s\n%s", stdout, stderr)
	}
}

func TestAgentSetup_NamesTheProfileItConnects(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		env       string // CLOUDRU_PROFILE
		agentKey  bool   // the profile agent holds a key of evo login
		wantWarns bool
	}{
		{"no --profile, agent has a login key", nil, "", true, true},
		{"--profile agent", []string{"--profile", "agent"}, "", true, false},
		// The person chose the default profile by name: no question of it.
		{"CLOUDRU_PROFILE=default", nil, config.DefaultProfile, true, false},
		{"no --profile, agent has a configure key", nil, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupCLI(t)
			t.Setenv("CLOUDRU_PROFILE", tc.env)
			if err := config.SaveProfile("default", config.Profile{}, config.Credentials{KeyID: "kd", Secret: "sd"}); err != nil {
				t.Fatal(err)
			}
			creds := config.Credentials{KeyID: "ka", Secret: "sa"}
			if tc.agentKey {
				creds.Login = &config.LoginKey{AccessKeyID: "ak"}
			}
			if err := config.SaveProfile("agent", config.Profile{}, creds); err != nil {
				t.Fatal(err)
			}
			args := append(append([]string{}, tc.args...), "agent", "setup", "--client", "cursor", "--print", "json")
			code, stdout, stderr := runCLI(t, "", args...)
			if code != 0 {
				t.Fatalf("setup: exit %d\n%s\n%s", code, stdout, stderr)
			}
			warns := strings.Contains(stderr, "--profile agent") && strings.Contains(stderr, config.DefaultProfile)
			if warns != tc.wantWarns {
				t.Errorf("warns of the profile it connects %v, want %v:\n%s", warns, tc.wantWarns, stderr)
			}
		})
	}
}
