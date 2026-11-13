// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// writeDesktopEntry writes the Claude Desktop file with an evo entry that
// starts command with args.
func writeDesktopEntry(t *testing.T, command string, args ...string) {
	t.Helper()
	path := clientPath(t, mustClient(t, "claude-desktop"), scopeUser)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// As agent setup writes it: the path of this system, JSON-escaped
	// (C:\\Users\\… on Windows).
	doc, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"other": map[string]any{"command": "x"},
		"evo":   map[string]any{"command": command, "args": args},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, doc, 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakeBinary is an existing file at dir/name.
func fakeBinary(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("not evo"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// План 1.9 (рекомендация «Риска», заключение 31, (б)): `evo doctor`
// проверяет запись evo у клиента, который запускает evo по полному пути
// (Claude Desktop): приложение не читает PATH и молча перестаёт запускать
// evo, если путь исчез после обновления или переноса, — ошибку показывает
// приложение, подсказки evo человек не видит. doctor называет: путь жив,
// это этот evo, профиль из --profile есть.
func TestDoctor_ChecksTheEntryOfAClientOfFullPath(t *testing.T) {
	home := setupAgent(t)
	this := fakeBinary(t, home, "evo")
	executable = func() (string, error) { return this, nil }

	one := func(t *testing.T) (tools.DoctorCheck, bool) {
		t.Helper()
		checks := agentEntryChecks()
		if len(checks) > 1 {
			t.Fatalf("%d checks of one entry: %+v", len(checks), checks)
		}
		if len(checks) == 0 {
			return tools.DoctorCheck{}, false
		}
		return checks[0], true
	}

	if ch, ok := one(t); ok {
		t.Errorf("no file of the app: a check %+v; nothing was set up, nothing to say", ch)
	}

	writeDesktopEntry(t, this, "mcp", "--toolsets", "vm")
	if ch, ok := one(t); !ok || ch.Status != tools.CheckOK || ch.Name != "Claude Desktop" || !strings.Contains(ch.Detail, this) {
		t.Errorf("an entry of this evo: %+v, want ok naming the path", ch)
	}

	gone := filepath.Join(home, "Cellar", "evo", "1.8.2", "bin", "evo")
	writeDesktopEntry(t, gone, "mcp")
	ch, ok := one(t)
	if !ok || ch.Status != tools.CheckWarn || !strings.Contains(ch.Detail, gone) || !strings.Contains(ch.Detail, "evo agent setup --client claude-desktop") {
		t.Errorf("an entry whose evo is gone: %+v, want a warning naming the path and the command that writes it again", ch)
	}
	if ch.Auth {
		t.Error("a broken entry of the app is not a broken key: doctor must not exit 2 for it")
	}

	other := fakeBinary(t, t.TempDir(), "evo")
	writeDesktopEntry(t, other, "mcp")
	if ch, ok := one(t); !ok || ch.Status != tools.CheckWarn || !strings.Contains(ch.Detail, other) || !strings.Contains(ch.Detail, "evo agent setup --client claude-desktop") {
		t.Errorf("an entry of another evo: %+v, want a warning naming it", ch)
	}

	for _, args := range [][]string{{"--profile", "ghost", "mcp"}, {"--profile=ghost", "mcp"}} {
		writeDesktopEntry(t, this, args...)
		if ch, ok := one(t); !ok || ch.Status != tools.CheckWarn || !strings.Contains(ch.Detail, "ghost") {
			t.Errorf("an entry of a profile that is not there (%q): %+v, want a warning naming the profile", args, ch)
		}
	}
	writeDesktopEntry(t, this, "--profile", "ghost", "mcp")
	// A profile of CLOUDRU_CONFIG_DIR is not the app's: it starts evo
	// without the shell's variables (docs/mcp.md).
	if err := config.SaveProfile("ghost", config.Profile{}, config.Credentials{KeyID: "k", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	if ch, ok := one(t); !ok || ch.Status != tools.CheckWarn || !strings.Contains(ch.Detail, config.DefaultDir()) {
		t.Errorf("a profile only in CLOUDRU_CONFIG_DIR: %+v, want a warning naming the directory the app reads", ch)
	}
	t.Setenv(config.EnvConfigDir, "")
	if err := config.SaveProfile("ghost", config.Profile{}, config.Credentials{KeyID: "k", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	if ch, ok := one(t); !ok || ch.Status != tools.CheckOK || !strings.Contains(ch.Detail, "ghost") {
		t.Errorf("an entry of a profile the app finds: %+v, want ok naming the profile", ch)
	}

	path := clientPath(t, mustClient(t, "claude-desktop"), scopeUser)
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"other":{"command":"x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if ch, ok := one(t); ok {
		t.Errorf("a file without evo's entry: %+v; evo was not connected there", ch)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if ch, ok := one(t); !ok || ch.Status != tools.CheckWarn || !strings.Contains(ch.Detail, path) {
		t.Errorf("a file the app cannot read either: %+v, want a warning naming it", ch)
	}

	found := false
	for _, ch := range doctorLocal(nil) {
		found = found || ch.Name == "Claude Desktop"
	}
	if !found {
		t.Error("evo doctor does not show the check")
	}

	hostOS = "linux"
	if ch, ok := one(t); ok {
		t.Errorf("linux, where the app has no build: %+v", ch)
	}
}

// doctor reads the entry of a client of fullPath as JSON: a client of
// another format would go unchecked without a word.
func TestDoctor_ClientsOfFullPathAreRead(t *testing.T) {
	for _, c := range agentClients {
		if c.fullPath && c.format != formatJSON {
			t.Errorf("%s starts evo by its full path, and its file is %s: agentEntryChecks reads JSON only", c.name, c.format)
		}
	}
}
