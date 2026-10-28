// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// G130-27: when the zone has no subnet, the provision hint names subnet_id
// and list_subnets; evo vm create words the same with --subnet-id.
func TestVMCreate_NoSubnetHintNamesSubnetID(t *testing.T) {
	withCloud(t, noSubnetCloud{previewCloud{}})
	code, out, errOut := runCLI(t, "", "vm", "create", "web", "--json")
	if code == 0 {
		t.Fatalf("want refusal, got ok:\n%s%s", out, errOut)
	}
	var env struct {
		Error struct {
			Reason string `json:"reason"`
			Hint   string `json:"hint"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(errOut), &env); err != nil {
		t.Fatalf("stderr %q: %v", errOut, err)
	}
	if env.Error.Reason != clerrors.CodeConfigInvalid {
		t.Errorf("reason %q, want CONFIG_INVALID; stderr:\n%s", env.Error.Reason, errOut)
	}
	if !strings.Contains(env.Error.Hint, "--subnet-id") {
		t.Errorf("CLI hint without --subnet-id: %q", env.Error.Hint)
	}
	if strings.Contains(env.Error.Hint, "inputSchema") {
		t.Errorf("CLI hint names inputSchema: %q", env.Error.Hint)
	}
}

// G130-35: a schema format:uuid refusal through a command names the flag
// or positional and --help, not inputSchema; tools call keeps inputSchema.
func TestCLI_FormatHintsSpeakCLILanguage(t *testing.T) {
	reg := newRegistry()
	root := newRootCmd()
	for _, tl := range reg.Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for arg, raw := range props {
			p, _ := raw.(map[string]any)
			format, _ := p["format"].(string)
			if format == "" {
				continue
			}
			bad := "not-a-uuid"
			if format == "cidr" {
				bad = "not-a-cidr"
			}
			err := tools.CheckArgs(tl, map[string]any{arg: bad})
			if err == nil {
				t.Errorf("%s.%s: CheckArgs accepted %q", tl.Name, arg, bad)
				continue
			}
			text := tools.ErrResult(err)
			var m map[string]any
			if json.Unmarshal([]byte(text), &m) != nil {
				t.Errorf("%s.%s: %s", tl.Name, arg, text)
				continue
			}
			if m["reason"] != clerrors.CodeUsage || m["argument"] != arg {
				t.Errorf("%s.%s: answer %s, want USAGE with argument", tl.Name, arg, text)
			}
			// tools call: inputSchema stays.
			var ce *clerrors.CLIError
			if !errors.As(toolError(text, callHints(tl.Name)), &ce) {
				t.Errorf("%s.%s: tools call: %+v", tl.Name, arg, ce)
				continue
			}
			if !strings.Contains(ce.Hint, "inputSchema") {
				t.Errorf("%s.%s tools call hint without inputSchema: %q", tl.Name, arg, ce.Hint)
			}
			if tl.CLI.Verb == "" {
				continue
			}
			path := []string{tl.CLI.Verb}
			if tl.CLI.Group != "" {
				path = []string{tl.CLI.Group, tl.CLI.Verb}
			}
			cmd, _, findErr := root.Find(path)
			if findErr != nil || cmd == nil {
				// Engine commands and odd paths: skip the CLI surface.
				continue
			}
			c := treeCmd{CLISpec: tl.CLI, Tool: tl.Name}
			hints := treeCommandHints(cmd, c)
			if !errors.As(toolError(text, hints), &ce) {
				t.Errorf("%s.%s command: %+v", tl.Name, arg, ce)
				continue
			}
			if strings.Contains(ce.Hint, "inputSchema") {
				t.Errorf("%s.%s CLI hint names inputSchema: %q", tl.Name, arg, ce.Hint)
			}
			how := hints.how(arg)
			if how == "" || !strings.Contains(ce.Hint, how) {
				t.Errorf("%s.%s CLI hint %q, want %q", tl.Name, arg, ce.Hint, how)
			}
			if !strings.Contains(ce.Hint, "--help") {
				t.Errorf("%s.%s CLI hint without --help: %q", tl.Name, arg, ce.Hint)
			}
		}
	}
}

// G130-35 smoke: evo vpc fip-release not-a-uuid --json.
func TestFipRelease_BadUUIDHint(t *testing.T) {
	i18n.SetLang(i18n.EN)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	code, _, errOut := runCLI(t, "", "vpc", "fip-release", "not-a-uuid", "--json")
	if code != clerrors.ExitGeneral {
		t.Fatalf("exit %d, want %d:\n%s", code, clerrors.ExitGeneral, errOut)
	}
	var env struct {
		Error struct {
			Reason string `json:"reason"`
			Hint   string `json:"hint"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(errOut), &env); err != nil {
		t.Fatalf("stderr %q: %v", errOut, err)
	}
	if env.Error.Reason != clerrors.CodeUsage {
		t.Errorf("reason %q", env.Error.Reason)
	}
	if strings.Contains(env.Error.Hint, "inputSchema") {
		t.Errorf("hint names inputSchema: %q", env.Error.Hint)
	}
	if !strings.Contains(env.Error.Hint, "id") || !strings.Contains(env.Error.Hint, "--help") {
		t.Errorf("hint %q, want id and --help", env.Error.Hint)
	}

	code, _, errOut = runCLI(t, "", "tools", "call", "release_floating_ip", "--arg", "id=not-a-uuid", "--json")
	if code != clerrors.ExitGeneral {
		t.Fatalf("tools call exit %d:\n%s", code, errOut)
	}
	if err := json.Unmarshal([]byte(errOut), &env); err != nil {
		t.Fatalf("tools call stderr %q: %v", errOut, err)
	}
	if env.Error.Reason != clerrors.CodeUsage || !strings.Contains(env.Error.Hint, "inputSchema") {
		t.Errorf("tools call: reason %q hint %q", env.Error.Reason, env.Error.Hint)
	}
}
