// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// QA-прогон 20.10: находки обвязки CLI. Облако подменено (setupCLI).

// cliJSONError is the {"error":…} line of a --json run's stderr.
func cliJSONError(t *testing.T, stderr string) (code int, reason, message string) {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		var e struct {
			Error *struct {
				Code    int    `json:"code"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(line), &e) == nil && e.Error != nil {
			return e.Error.Code, e.Error.Reason, e.Error.Message
		}
	}
	t.Fatalf("no {\"error\":…} in stderr: %q", stderr)
	return 0, "", ""
}

// QA-12: 'evo --json tools call' with a tool error leaves stdout empty, as
// the command trees do: pg_get without an id printed its error body to
// stdout too, ar_get did not. Without --json the body is still printed.
func TestQA12_ToolsCallJSONErrorOnlyStderr(t *testing.T) {
	setupCLI(t)
	for _, tool := range []string{"pg_get", "ar_get"} {
		code, stdout, stderr := runCLI(t, "", "--json", "tools", "call", tool)
		if code != clerrors.ExitGeneral || strings.TrimSpace(stdout) != "" {
			t.Errorf("%s: exit %d, stdout %q", tool, code, stdout)
			continue
		}
		if c, reason, _ := cliJSONError(t, stderr); c != code || reason != clerrors.CodeUsage {
			t.Errorf("%s: stderr error %d %s", tool, c, reason)
		}
	}
	code, stdout, _ := runCLI(t, "", "tools", "call", "pg_get")
	if code != clerrors.ExitGeneral || !strings.Contains(stdout, `"reason": "USAGE"`) {
		t.Errorf("text mode: exit %d, stdout %q; want the tool's answer", code, stdout)
	}
}

// QA-14: --disk-size -5 is USAGE before anything reaches the cloud, not a
// 10 GB disk.
func TestQA14_DeployNegativeDiskSize(t *testing.T) {
	fc := setupCLI(t)
	code, stdout, stderr := runCLI(t, "", "--json", "deploy", "--disk-size", "-5", "--yes")
	c, reason, msg := cliJSONError(t, stderr)
	if code != clerrors.ExitGeneral || c != code || reason != clerrors.CodeUsage || !strings.Contains(msg, "-5") {
		t.Errorf("exit %d, error %d %s %q; stdout %q", code, c, reason, msg, stdout)
	}
	if n := fc.iamCalls.Load(); n != 0 {
		t.Errorf("IAM asked %d times", n)
	}
}

// QA-17: an unknown flag before the command is "unknown flag", not
// "unknown command" for the word after it (cobra took the flag to have a
// value and dropped the command name with it).
func TestQA17_UnknownFlagBeforeCommand(t *testing.T) {
	setupCLI(t)
	for _, tc := range []struct {
		args []string
		flag string
	}{
		{[]string{"--bogus", "vm", "list"}, "--bogus"},
		{[]string{"--json", "--bogus", "vm", "list"}, "--bogus"},
		{[]string{"--profile", "p", "--bogus", "disk", "delete", "X"}, "--bogus"},
		// --non-interactive is a flag of deploy and init only.
		{[]string{"--non-interactive", "disk", "delete", "X"}, "--non-interactive"},
		{[]string{"-z", "vm", "list"}, "-z"},
		{[]string{"-yz", "vm", "list"}, "'z'"},
	} {
		code, _, stderr := runCLI(t, "", append([]string{"--json"}, tc.args...)...)
		c, reason, msg := cliJSONError(t, stderr)
		if code != clerrors.ExitGeneral || c != code || reason != clerrors.CodeUsage ||
			!strings.Contains(msg, "unknown") || !strings.Contains(msg, strings.Trim(tc.flag, "'")) || strings.Contains(msg, "command") {
			t.Errorf("%v: exit %d, %s %q", tc.args, code, reason, msg)
		}
	}
	// A typo in the command name is still an unknown command, flags or not.
	for _, args := range [][]string{{"lst"}, {"--profile", "p", "vm", "lst"}, {"--json", "-y", "lst"}} {
		code, _, stderr := runCLI(t, "", append([]string{"--json"}, args...)...)
		if _, reason, msg := cliJSONError(t, stderr); code != clerrors.ExitGeneral || reason != clerrors.CodeUsage || !strings.Contains(msg, `unknown command "lst"`) {
			t.Errorf("%v: exit %d, %s %q", args, code, reason, msg)
		}
	}
}

func TestLeadingUnknownFlag(t *testing.T) {
	root := newRootCmd()
	for _, tc := range []struct {
		args []string
		want string // "" — no error
	}{
		{[]string{"vm", "list", "--bogus"}, ""}, // after the command: cobra names it itself
		{[]string{"--profile", "--bogus", "vm"}, ""},
		{[]string{"--profile=p", "--bogus", "vm"}, "unknown flag: --bogus"},
		{[]string{"--json", "-y", "--dry-run", "--project-id", "x", "--region=z", "vm"}, ""},
		{[]string{"-h", "vm"}, ""},
		{[]string{"--version"}, ""},
		{[]string{"--", "--bogus"}, ""},
		{[]string{"--bogus=1", "vm"}, "unknown flag: --bogus"},
		{[]string{"-q", "vm"}, "unknown shorthand flag: 'q' in -q"},
	} {
		err := leadingUnknownFlag(root, tc.args)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != tc.want {
			t.Errorf("%v: %q, want %q", tc.args, got, tc.want)
		}
	}
}
