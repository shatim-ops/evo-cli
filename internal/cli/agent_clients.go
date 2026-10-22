// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/shatim-ops/evo-cli/internal/agentconf"
)

// The MCP clients 'evo agent setup' knows, as data: where each keeps its
// servers, under which key, and how it spells an entry. A client is a row
// of agentClients; the code reads the table and has no case per client.
// The owner's decisions on the defaults (docs/internal/agent-setup/spec.md,
// В-1…В-4) are the constants and the readOnly column below.

// agentScope is where a configuration lives: the project (the working
// directory) or the user's home.
type agentScope string

const (
	scopeProject agentScope = "project"
	scopeUser    agentScope = "user"
)

// defaultScope is the scope without --scope (В-1, D22): the project, so
// what is connected is in sight and nothing lives globally. A client may
// name another (agentClient.scope), and one without a file of its scope
// uses the one it has.
const defaultScope = scopeProject

// defaultToolsets are the groups the agent sees without --toolsets (В-2):
// deploy and the VM with its network, disks and images, not all ~150 tools.
var defaultToolsets = []string{"deploy", "vm", "vpc", "disk", "image"}

// configFormat is the language of a client's configuration file.
type configFormat string

const (
	formatJSON configFormat = "json"
	formatTOML configFormat = "toml"
)

// baseDir is the directory a client's file is under.
type baseDir int

const (
	inWorkDir    baseDir = iota // the working directory: the project
	inHome                      // os.UserHomeDir
	inUserConfig                // os.UserConfigDir: %APPDATA% on Windows, ~/Library/Application Support on macOS
)

func (b baseDir) dir() (string, error) {
	switch b {
	case inHome:
		return os.UserHomeDir()
	case inUserConfig:
		return os.UserConfigDir()
	}
	return os.Getwd()
}

// clientFile is a file of a client: rel (slash-separated) under base.
type clientFile struct {
	base baseDir
	rel  string
}

func (f clientFile) path() (string, error) {
	dir, err := f.base.dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.FromSlash(f.rel)), nil
}

// skillFile is where --skill puts the rules: a file all evo's (SKILL.md,
// with its front matter) or a section of a file the user keeps (AGENTS.md).
type skillFile struct {
	clientFile
	whole bool
}

// agentClient describes one MCP client.
type agentClient struct {
	name  string // --client
	title string // as the client calls itself
	docs  string // the client's documentation of the format

	format  configFormat
	servers string // the object (JSON) or table (TOML) of the servers
	// comments: the client reads its JSON with comments and trailing
	// commas (JSONC), so a file may have them.
	comments bool
	// config are the client's files by scope; none: the client's file is
	// not on this machine, and the entry is printed (--print).
	config map[agentScope]clientFile
	skill  map[agentScope]skillFile
	// scope is the scope without --scope, when not defaultScope: the file
	// the client reads whatever the project.
	scope agentScope

	// How the client spells an entry.
	stdioType string // "type" of a stdio entry; "" for none
	httpType  string // "type" of an HTTP entry; "" for none
	urlKey    string // the key of the server's URL
	// tokenRef is how the client reads an environment variable in a
	// header, a format of the variable's name; "" when it cannot, and
	// the header gets a placeholder to replace.
	tokenRef string
	// tokenEnvKey: the client takes the name of the token's variable in
	// this key instead of a header (Codex: bearer_token_env_var).
	tokenEnvKey string

	// readOnly: --read-only by default (В-4) — the client is an agent
	// nobody watches, which sets confirmed=true itself.
	readOnly bool
	// noteEN, noteRU: what to know about the client after setup.
	noteEN, noteRU string
}

// agentClients are the clients of 'evo agent setup', by --client.
var agentClients = []agentClient{
	{
		name: "claude-code", title: "Claude Code", docs: "https://code.claude.com/docs/en/mcp",
		format: formatJSON, servers: "mcpServers",
		config: map[agentScope]clientFile{scopeProject: {inWorkDir, ".mcp.json"}, scopeUser: {inHome, ".claude.json"}},
		skill: map[agentScope]skillFile{
			scopeProject: {clientFile{inWorkDir, ".claude/skills/evo/SKILL.md"}, true},
			scopeUser:    {clientFile{inHome, ".claude/skills/evo/SKILL.md"}, true},
		},
		httpType: "http", urlKey: "url", tokenRef: "${%s}",
	},
	{
		name: "cursor", title: "Cursor", docs: "https://cursor.com/docs/context/mcp",
		format: formatJSON, servers: "mcpServers",
		config: map[agentScope]clientFile{scopeProject: {inWorkDir, ".cursor/mcp.json"}, scopeUser: {inHome, ".cursor/mcp.json"}},
		skill:  map[agentScope]skillFile{scopeProject: {clientFile{inWorkDir, "AGENTS.md"}, false}},
		urlKey: "url", tokenRef: "${env:%s}",
	},
	{
		name: "vscode", title: "VS Code", docs: "https://code.visualstudio.com/docs/copilot/reference/mcp-configuration",
		format: formatJSON, servers: "servers", comments: true,
		config:    map[agentScope]clientFile{scopeProject: {inWorkDir, ".vscode/mcp.json"}, scopeUser: {inUserConfig, "Code/User/mcp.json"}},
		skill:     map[agentScope]skillFile{scopeProject: {clientFile{inWorkDir, "AGENTS.md"}, false}},
		stdioType: "stdio", httpType: "http", urlKey: "url", tokenRef: "${env:%s}",
	},
	{
		name: "gemini", title: "Gemini CLI", docs: "https://geminicli.com/docs/tools/mcp-server/",
		format: formatJSON, servers: "mcpServers",
		config: map[agentScope]clientFile{scopeProject: {inWorkDir, ".gemini/settings.json"}, scopeUser: {inHome, ".gemini/settings.json"}},
		skill: map[agentScope]skillFile{
			scopeProject: {clientFile{inWorkDir, "GEMINI.md"}, false},
			scopeUser:    {clientFile{inHome, ".gemini/GEMINI.md"}, false},
		},
		urlKey: "httpUrl", tokenRef: "${%s}",
	},
	{
		name: "codex", title: "Codex", docs: "https://developers.openai.com/codex/mcp",
		format: formatTOML, servers: "mcp_servers",
		config: map[agentScope]clientFile{scopeProject: {inWorkDir, ".codex/config.toml"}, scopeUser: {inHome, ".codex/config.toml"}},
		skill: map[agentScope]skillFile{
			scopeProject: {clientFile{inWorkDir, "AGENTS.md"}, false},
			scopeUser:    {clientFile{inHome, ".codex/AGENTS.md"}, false},
		},
		// Codex reads .codex/config.toml of a trusted project only (D22).
		scope:  scopeUser,
		urlKey: "url", tokenEnvKey: "bearer_token_env_var",
		noteEN: "Codex reads .codex/config.toml of a project only when the project is trusted; --scope project writes that file.",
		noteRU: "Codex читает .codex/config.toml проекта, только если проект доверенный; --scope project пишет этот файл.",
	},
	{
		// The path of the agent's mcp.json depends on where it runs (a VM,
		// Container Apps, AI Agents) and is not confirmed by public
		// documentation (В-3): the entry is printed.
		name: "gigaagent", title: "ГигаАгент",
		format: formatJSON, servers: "mcpServers",
		httpType: "http", urlKey: "url",
		readOnly: true,
		noteEN:   "Put this into the agent's mcp.json, and run 'evo configure' as the user the agent runs evo as (docs/mcp.md: the autonomous read-only agent).",
		noteRU:   "Вставьте это в mcp.json агента и выполните 'evo configure' под тем пользователем, от имени которого агент запускает evo (docs/mcp.md, «Автономный агент только на чтение»).",
	},
}

// lookupClient finds the client named name.
func lookupClient(name string) (agentClient, bool) {
	for _, c := range agentClients {
		if c.name == name {
			return c, true
		}
	}
	return agentClient{}, false
}

// clientNames are the names of agentClients, sorted.
func clientNames() []string {
	names := make([]string, len(agentClients))
	for i, c := range agentClients {
		names[i] = c.name
	}
	sort.Strings(names)
	return names
}

// defaultScope is the scope the client gets without --scope.
func (c agentClient) defaultScope() agentScope {
	if c.scope != "" {
		return c.scope
	}
	return defaultScope
}

// scopes are the scopes the client has files of, project first.
func (c agentClient) scopes() []agentScope {
	var out []agentScope
	for _, s := range []agentScope{scopeProject, scopeUser} {
		if _, ok := c.config[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

// serverSpec is the evo server the entry starts or reaches.
type serverSpec struct {
	args []string // stdio: the arguments of evo
	url  string   // HTTP: the server's MCP endpoint
}

// evoCommand is the command a client starts: evo from its PATH, as in the
// recipes of docs/mcp.md.
const evoCommand = "evo"

// entry is the client's entry of the server. It holds no secret: the
// Cloud.ru key stays in the profile of 'evo configure', and the bearer
// token of HTTP is named, never written (placeholder reports whether the
// header has a placeholder to replace instead).
func (c agentClient) entry(s serverSpec) (e agentconf.Entry, placeholder bool) {
	if s.url == "" {
		if c.stdioType != "" {
			e = append(e, agentconf.Field{Key: "type", Value: c.stdioType})
		}
		return append(e, agentconf.Field{Key: "command", Value: evoCommand}, agentconf.Field{Key: "args", Value: s.args}), false
	}
	if c.httpType != "" {
		e = append(e, agentconf.Field{Key: "type", Value: c.httpType})
	}
	e = append(e, agentconf.Field{Key: c.urlKey, Value: s.url})
	if c.tokenEnvKey != "" {
		return append(e, agentconf.Field{Key: c.tokenEnvKey, Value: envBearerToken}), false
	}
	token, placeholder := "<"+envBearerToken+">", true
	if c.tokenRef != "" {
		token, placeholder = fmt.Sprintf(c.tokenRef, envBearerToken), false
	}
	return append(e, agentconf.Field{Key: "headers", Value: agentconf.Entry{{Key: "Authorization", Value: "Bearer " + token}}}), placeholder
}

// render is a configuration file holding only the entry, in format: under
// the client's servers key in its own format, else under the key most
// clients of that format share.
func (c agentClient) render(format configFormat, e agentconf.Entry) []byte {
	servers := c.servers
	if format != c.format {
		servers = sharedServers[format]
	}
	if format == formatTOML {
		return agentconf.TOMLDocument([]string{servers, mcpServerName}, e)
	}
	return agentconf.JSONDocument(servers, mcpServerName, e)
}

// merge puts the entry into the client's configuration file old. Only the
// keys evo writes are evo's: what the user added to the entry stays.
func (c agentClient) merge(old []byte, e agentconf.Entry) (agentconf.Result, error) {
	if c.format == formatTOML {
		return agentconf.TOML(old, []string{c.servers, mcpServerName}, e, c.ownedKeys())
	}
	return agentconf.JSON(old, c.servers, mcpServerName, e, c.ownedKeys(), c.comments)
}

// ownedKeys are the keys of the client's entry evo writes, of either
// transport: the stdio entry and the HTTP one, as entry spells them.
func (c agentClient) ownedKeys() []string {
	var keys []string
	for _, s := range []serverSpec{{}, {url: "http://evo"}} {
		e, _ := c.entry(s)
		for _, f := range e {
			if !slices.Contains(keys, f.Key) {
				keys = append(keys, f.Key)
			}
		}
	}
	return keys
}

// mcpServerName is the name of evo's entry in a client's servers.
const mcpServerName = "evo"

// sharedServers is the servers key most clients of a format use.
var sharedServers = map[configFormat]string{formatJSON: "mcpServers", formatTOML: "mcp_servers"}
