// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// clientReads is where each client of 'evo agent setup' reads its MCP
// configuration, by scope, written from the client's own documentation
// (agentClient.docs) and not from agentClients: the golden files of
// TestAgentSetup_Golden are made by the code they check
// (-update-golden), so a wrong path would pass them. A path is under the
// HOME and the working directory of setupAgent; userConfig is the user's
// configuration directory of this OS. A client that only prints its entry
// has an empty row.
//
// Release plan 1.8, «Подключение агента одной вставкой — часть в 1.8»: a
// client added to agentClients (claude-desktop) without a row here fails
// the build — the row is the test of where it writes.
var clientReads = map[string]map[agentScope]func(home, work, userConfig string) string{
	"claude-code": {
		scopeProject: func(_, w, _ string) string { return filepath.Join(w, ".mcp.json") },
		scopeUser:    func(h, _, _ string) string { return filepath.Join(h, ".claude.json") },
	},
	"cursor": {
		scopeProject: func(_, w, _ string) string { return filepath.Join(w, ".cursor", "mcp.json") },
		scopeUser:    func(h, _, _ string) string { return filepath.Join(h, ".cursor", "mcp.json") },
	},
	"vscode": {
		scopeProject: func(_, w, _ string) string { return filepath.Join(w, ".vscode", "mcp.json") },
		scopeUser:    func(_, _, u string) string { return filepath.Join(u, "Code", "User", "mcp.json") },
	},
	"gemini": {
		scopeProject: func(_, w, _ string) string { return filepath.Join(w, ".gemini", "settings.json") },
		scopeUser:    func(h, _, _ string) string { return filepath.Join(h, ".gemini", "settings.json") },
	},
	"codex": {
		scopeProject: func(_, w, _ string) string { return filepath.Join(w, ".codex", "config.toml") },
		scopeUser:    func(h, _, _ string) string { return filepath.Join(h, ".codex", "config.toml") },
	},
	"gigaagent": {},
}

// userConfigDir is the user's configuration directory of this OS under the
// environment of setupAgent, as the clients' documentation names it.
func userConfigDir(home string) string {
	switch runtime.GOOS {
	case "windows":
		return os.Getenv("APPDATA")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	}
	return os.Getenv("XDG_CONFIG_HOME")
}

// Each client of agentClients has a row of clientReads, and for each of its
// scopes 'evo agent setup' writes the evo entry exactly where the client
// reads it; a repeat changes nothing (no second entry); --dry-run writes
// nothing. A client that only prints has no file to write.
func TestAgentClients_WriteWhereTheClientReads(t *testing.T) {
	var names []string
	for _, c := range agentClients {
		names = append(names, c.name)
		rows, ok := clientReads[c.name]
		if !ok {
			t.Errorf("%s: a client of agentClients without a row of clientReads — add where it reads its file, from %s, as the test of where evo writes", c.name, c.docs)
			continue
		}
		if len(rows) != len(c.scopes()) {
			t.Errorf("%s: clientReads has %d scope(s), the client %d", c.name, len(rows), len(c.scopes()))
		}
		for _, s := range c.scopes() {
			want := rows[s]
			if want == nil {
				t.Errorf("%s: no clientReads row for scope %s", c.name, s)
				continue
			}
			t.Run(c.name+"/"+string(s), func(t *testing.T) {
				home := setupAgent(t)
				work, _ := os.Getwd()
				path := want(home, work, userConfigDir(home))
				args := []string{"agent", "setup", "--client", c.name, "--scope", string(s)}

				if code, _, stderr := runCLI(t, "", append(args, "--dry-run")...); code != 0 {
					t.Fatalf("--dry-run: exit %d: %s", code, stderr)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("--dry-run wrote %s (%v)", path, err)
				}

				if code, _, stderr := runCLI(t, "", append(args, "--yes")...); code != 0 {
					t.Fatalf("exit %d: %s", code, stderr)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("evo did not write where %s reads (%s): %v", c.title, path, err)
				}
				text := string(data)
				if !holdsEvoEntry(c, data) {
					t.Fatalf("%s: no %s.%s entry running evo mcp:\n%s", path, c.servers, mcpServerName, text)
				}

				code, stdout, stderr := runCLI(t, "", append(args, "--yes")...)
				if code != 0 || !strings.Contains(stdout, "nothing changed") {
					t.Fatalf("repeat: exit %d: %s%s", code, stdout, stderr)
				}
				if again, _ := os.ReadFile(path); string(again) != text {
					t.Errorf("repeat changed %s:\n%s", path, again)
				}
			})
		}
	}
	for name := range clientReads {
		if !slices.Contains(names, name) {
			t.Errorf("clientReads has %s, which agentClients has not: take the row out", name)
		}
	}
}

// holdsEvoEntry: the file has the evo entry under the client's servers key,
// and the entry runs evo mcp (a command and its args, or the TOML table).
func holdsEvoEntry(c agentClient, data []byte) bool {
	if c.format == formatTOML {
		text := string(data)
		return strings.Count(text, "["+c.servers+"."+mcpServerName+"]") == 1 && strings.Contains(text, `"mcp"`)
	}
	var doc map[string]map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	e, ok := doc[c.servers][mcpServerName]
	return ok && e.Command != "" && slices.Contains(e.Args, "mcp")
}
