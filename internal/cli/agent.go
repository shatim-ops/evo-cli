// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/agentconf"
	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// evo agent connects evo to an AI agent (docs/internal/agent-setup/spec.md):
// setup writes the evo MCP server into the configuration of the agent's
// MCP client, skill prints the rules of evo for the agent. Both are local
// commands: they write or print files of the user's machine, which an MCP
// server has no business doing.

func newAgentCmd() *cobra.Command {
	cmd := asGroup(&cobra.Command{
		Use:   "agent",
		Short: i18n.T("Connect evo to an AI agent: its MCP client and the rules for the agent", "Подключить evo к ИИ-агенту: MCP-клиент и правила для агента"),
	})
	cmd.AddCommand(newAgentSetupCmd(), newAgentSkillCmd())
	return cmd
}

func newAgentSkillCmd() *cobra.Command {
	var readOnly bool
	var group string
	cmd := &cobra.Command{
		Use:   "skill",
		Short: i18n.T("Print the rules of evo for an AI agent (SKILL.md)", "Правила работы с evo для ИИ-агента (SKILL.md)"),
		Long: i18n.T(`Print the rules of evo for an AI agent, as SKILL.md: always --json, what exit
code 3 and the field approval mean, --dry-run before a change, which answers
are text or untrusted, the tool groups. The rules are built from the tool
registry of this binary; docs/agent-skill.md is the same text. With --json:
{"skill": "<the text>"}. --read-only gives the rules of an agent that changes
nothing, through the CLI too: those 'evo agent setup --read-only --skill'
writes.

'evo agent setup --skill' puts them where the client reads them.`,
			`Печатает правила работы с evo для ИИ-агента в виде SKILL.md: всегда --json,
что значат код выхода 3 и поле approval, --dry-run перед изменением, какие
ответы — текст или недоверенные данные, группы инструментов. Правила
собираются из реестра инструментов этого бинаря; docs/agent-skill.md — тот же
текст. Текст английский: его читает агент. С --json: {"skill": "<текст>"}.
--read-only — правила агента, который ничего не меняет, в том числе через
CLI: их пишет 'evo agent setup --read-only --skill'.

'evo agent setup --skill' кладёт их туда, где их читает клиент.`),
		Example: `  evo agent skill
  evo agent skill --read-only > AGENTS.md
  evo agent skill --group pg`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			text := agentSkill(cmd.Root(), readOnly)
			if cmd.Flags().Changed("group") {
				card, ok := tools.GroupCard(group)
				if !ok {
					return clerrors.New(clerrors.CodeUsage, i18n.Tf("unknown group %q", "неизвестная группа %q", group),
						i18n.Tf("Groups: %s.", "Группы: %s.", strings.Join(tools.Groups(), ", ")))
				}
				text = card
			}
			if p := cmdPrinter(cmd); p.IsJSON() {
				return p.JSON(map[string]string{"skill": text})
			}
			_, err := io.WriteString(cmd.OutOrStdout(), text)
			return err
		},
	}
	cmd.Flags().StringVar(&group, "group", "", i18n.T("Print the card of a tool group instead: its tools, commands, where ids come from, what deletes what a create makes",
		"Напечатать карточку группы инструментов: инструменты, команды, откуда id, чем удаляется созданное"))
	cmd.Flags().BoolVar(&readOnly, "read-only", false, i18n.T("The rules of an agent that changes nothing in the cloud, through the CLI too",
		"Правила агента, который ничего не меняет в облаке, в том числе через CLI"))
	return cmd
}

type agentSetupOptions struct {
	client   string
	scope    string
	toolsets []string
	readOnly bool
	httpURL  string
	print    string
	skill    bool
	force    bool
	// readOnlySet: --read-only was given, so the client's default does
	// not count.
	readOnlySet bool
}

func newAgentSetupCmd() *cobra.Command {
	var o agentSetupOptions
	cmd := &cobra.Command{
		Use:   "setup --client <client>",
		Short: i18n.T("Add the evo MCP server to the configuration of an MCP client", "Добавить MCP-сервер evo в настройки MCP-клиента"),
		Long:  agentSetupHelp(),
		Example: `  evo agent setup --client claude-code --dry-run
  evo agent setup --client cursor --scope user --yes
  evo agent setup --client codex --toolsets deploy,vm,vpc,k8s
  evo agent setup --client claude-code --skill
  evo agent setup --client gigaagent
  evo agent setup --client vscode --http http://127.0.0.1:8080/mcp`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.readOnlySet = cmd.Flags().Changed("read-only")
			return runAgentSetup(cmd, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.client, "client", "", i18n.T("MCP client: "+strings.Join(clientNames(), ", "), "MCP-клиент: "+strings.Join(clientNames(), ", ")))
	f.StringVar(&o.scope, "scope", "", i18n.T("Whose configuration: project (the working directory; the default) or user (the home directory; the default for codex)",
		"Чьи настройки: project (текущий каталог; по умолчанию) или user (домашний каталог; по умолчанию у codex)"))
	f.StringSliceVar(&o.toolsets, "toolsets", defaultToolsets, i18n.T("Tool groups the agent sees (comma-separated; all: every group; see 'evo tools list')",
		"Группы инструментов, которые видит агент (через запятую; all — все; группы — в 'evo tools list')"))
	f.BoolVar(&o.readOnly, "read-only", false, i18n.T("Run the server with --read-only: no change in the cloud, no secret (default for gigaagent)",
		"Запускать сервер с --read-only: без изменений в облаке и без секретов (у gigaagent — по умолчанию)"))
	f.StringVar(&o.httpURL, "http", "", i18n.T("Reach a running 'evo mcp --http' server at this URL instead of starting evo (e.g. http://127.0.0.1:8080/mcp)",
		"Подключаться к запущенному серверу 'evo mcp --http' по этому адресу, а не запускать evo (например, http://127.0.0.1:8080/mcp)"))
	f.StringVar(&o.print, "print", "", i18n.T("Print the entry as json or toml and write nothing (for a client not listed)",
		"Напечатать запись в формате json или toml и ничего не записывать (для клиента не из списка)"))
	f.BoolVar(&o.skill, "skill", false, i18n.T("Also write the rules of evo for the agent (SKILL.md or a section of AGENTS.md)",
		"Записать и правила работы с evo для агента (SKILL.md или раздел AGENTS.md)"))
	f.BoolVar(&o.force, "force", false, i18n.T("Replace an evo entry that differs from this one",
		"Заменить запись evo, которая отличается от этой"))
	_ = cmd.RegisterFlagCompletionFunc("client", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return clientNames(), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("scope", cobra.FixedCompletions([]string{string(scopeProject), string(scopeUser)}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("print", cobra.FixedCompletions([]string{string(formatJSON), string(formatTOML)}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

// agentSetupHelp is the long help, with the table of the clients.
func agentSetupHelp() string {
	width := 0
	for _, c := range agentClients {
		if f, ok := c.config[scopeProject]; ok {
			width = max(width, len(f.shown()))
		}
	}
	var rows strings.Builder
	for _, c := range agentClients {
		project, user := c.config[scopeProject], c.config[scopeUser]
		switch {
		case len(c.config) == 0:
			fmt.Fprintf(&rows, "  %-12s %s\n", c.name, i18n.T("printed: the agent's mcp.json is on its own machine", "печатается: mcp.json агента — на его машине"))
		case user.rel == "":
			fmt.Fprintf(&rows, "  %-12s %s\n", c.name, project.shown())
		default:
			fmt.Fprintf(&rows, "  %-12s %-*s  %s\n", c.name, width, project.shown(), user.shown())
		}
	}
	return i18n.Tf(`Add the evo MCP server to the configuration file of an MCP client.

The file is merged, not overwritten: other servers and keys stay as they are.
The command shows the diff and asks before it writes; without a terminal it
needs --yes (else exit code 3), --dry-run only shows the diff. An evo entry
that differs from this one is replaced only with --force (else exit code 1);
a broken file is left alone (exit code 2, with the line). The write is atomic
and keeps the file's mode; a new file gets 0600. The Cloud.ru key is not
written: the server takes it from the profile of 'evo configure' (exit code 2
when there is no key at all).

Clients and their files (--scope project, the default; --scope user, the
default for codex):
%s
Another client: --print json or --print toml prints the entry, writes nothing.

The agent sees the groups %s
unless --toolsets says otherwise (all: every group). --read-only, the default
for gigaagent, makes the server refuse every change and secret, and the rules
of --skill forbid a change through the CLI too; the server is all it bounds:
an agent nobody watches also needs a key with a viewer role only. --http URL connects the client to a running 'evo mcp --http'
server; the file names $%s instead of holding the token.

--skill also writes the rules of evo for the agent ('evo agent skill'):
.claude/skills/evo/SKILL.md for claude-code, a section of AGENTS.md (GEMINI.md
for gemini) for the others. The rules are evo's: the next run puts the rules
of this evo there, without --force.`,
		`Добавить MCP-сервер evo в файл настроек MCP-клиента.

Файл дополняется, а не перезаписывается: другие серверы и ключи остаются как
были. Перед записью команда показывает diff и спрашивает; без терминала нужен
--yes (иначе код выхода 3), --dry-run только показывает diff. Запись evo,
которая отличается от этой, заменяется только с --force (иначе код 1); битый
файл не трогается (код 2, с номером строки). Запись атомарная и сохраняет права
файла; новый файл — с правами 0600. Ключ Cloud.ru в файл не пишется: сервер
берёт его из профиля 'evo configure' (код 2, если ключа нет совсем).

Клиенты и их файлы (--scope project — по умолчанию; --scope user — по
умолчанию у codex):
%s
Другой клиент: --print json или --print toml печатает запись и ничего не пишет.

Агент видит группы %s,
если --toolsets не задаёт другие (all — все группы).
--read-only (у gigaagent — по умолчанию): сервер отклоняет любые изменения и
секреты, а правила --skill запрещают изменения и через CLI; ограничивает он
только сервер: агенту без присмотра нужен ещё ключ только с ролью
наблюдателя. --http URL подключает клиент к запущенному
серверу 'evo mcp --http'; в файле — ссылка на $%s, а не сам токен.

--skill дополнительно записывает правила работы с evo для агента ('evo agent
skill'): .claude/skills/evo/SKILL.md для claude-code, раздел AGENTS.md
(GEMINI.md для gemini) для остальных. Правила принадлежат evo: следующий
запуск кладёт туда правила этой версии evo, без --force.`,
		strings.TrimRight(rows.String(), "\n"), strings.Join(defaultToolsets, ","), envBearerToken)
}

// shown is the file as the help names it: relative to the project, under ~
// or the user configuration directory.
func (f clientFile) shown() string {
	switch f.base {
	case inHome:
		return "~/" + f.rel
	case inUserConfig:
		return i18n.T("<user config dir>/", "<каталог настроек>/") + f.rel
	}
	return f.rel
}

// agentServer is the server the entry starts or reaches, and how.
type agentServer struct {
	spec     serverSpec
	toolsets []string // nil: every group
	readOnly bool
}

// server checks the server options against the client.
func (o agentSetupOptions) server(c agentClient) (agentServer, error) {
	groups, err := tools.ParseToolsets(o.toolsets)
	if err != nil {
		return agentServer{}, clerrors.Wrap(clerrors.CodeUsage, i18n.Tf("--toolsets: %v", "--toolsets: %v", err),
			i18n.T("Groups are listed by 'evo tools list'.", "Группы — в выводе 'evo tools list'."), err)
	}
	s := agentServer{toolsets: groups, readOnly: c.readOnly}
	if o.readOnlySet {
		s.readOnly = o.readOnly
	}
	if o.httpURL == "" {
		s.spec.args = s.mcpArgs()
		return s, nil
	}
	if u, err := url.Parse(o.httpURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return agentServer{}, clerrors.New(clerrors.CodeUsage,
			i18n.Tf("--http %q is not an http(s) URL", "--http %q — не адрес http(s)", o.httpURL),
			i18n.T("Give the MCP endpoint of the server, e.g. --http http://127.0.0.1:8080/mcp",
				"Укажите адрес MCP сервера, например --http http://127.0.0.1:8080/mcp"))
	}
	s.spec.url = o.httpURL
	return s, nil
}

// mcpArgs are the arguments the client starts evo with: the profile,
// 'mcp' and the server's flags.
func (s agentServer) mcpArgs() []string {
	return append(append(profileArgs(), "mcp"), s.serverFlags()...)
}

// profileArgs name the profile, unless it is the default one: the client
// may not pass CLOUDRU_PROFILE on to evo.
func profileArgs() []string {
	if name, _ := config.ProfileName(flagProfile); name != config.DefaultProfile {
		return []string{"--profile", name}
	}
	return nil
}

// serverFlags are the flags of 'evo mcp' that limit what the agent gets.
func (s agentServer) serverFlags() []string {
	var flags []string
	if s.readOnly {
		flags = append(flags, "--read-only")
	}
	if s.toolsets != nil {
		flags = append(flags, "--toolsets", strings.Join(s.toolsets, ","))
	}
	return flags
}

// scopeFor is the scope to write: --scope when given and the client has
// it, else the client's default or, without a file there, its only one.
func (c agentClient) scopeFor(scope string) (agentScope, error) {
	have := c.scopes()
	given := scope != ""
	want := agentScope(scope)
	if !given {
		want = c.defaultScope()
	}
	if want != scopeProject && want != scopeUser {
		return "", clerrors.New(clerrors.CodeUsage, i18n.Tf("--scope %q: project or user", "--scope %q: project или user", scope), "")
	}
	switch {
	case slices.Contains(have, want):
		return want, nil
	case !given && len(have) > 0:
		return have[0], nil
	}
	return "", clerrors.New(clerrors.CodeUsage,
		i18n.Tf("%s has no %s configuration", "у %s нет настроек уровня %s", c.title, want),
		i18n.Tf("Leave out --scope, or print the entry: --print %s", "Уберите --scope или напечатайте запись: --print %s", c.format))
}

// agentTarget is a file the setup writes, and how evo's part goes in.
type agentTarget struct {
	path string
	// mode is the mode of a new file: 0600 for a client's configuration
	// (the user may add a token to it), 0644 for the rules, a file of the
	// project like any other. A file that is there keeps its own.
	mode  fs.FileMode
	merge func(old []byte) (agentconf.Result, error)
	// generated: evo's part is generated by evo (the rules), so a newer
	// one replaces it without --force, after the diff and consent; an evo
	// entry of a client's file may be the user's choice.
	generated bool
}

// agentChange is a target as read, with evo's part put in.
type agentChange struct {
	agentTarget
	old    []byte
	exists bool
	perm   fs.FileMode
	result agentconf.Result
}

func runAgentSetup(cmd *cobra.Command, o agentSetupOptions) error {
	c, ok := lookupClient(o.client)
	if !ok {
		msg := i18n.T("--client is required", "нужен --client")
		if o.client != "" {
			msg = i18n.Tf("unknown client %q", "неизвестный клиент %q", o.client)
		}
		return clerrors.New(clerrors.CodeUsage, msg,
			i18n.Tf("Clients: %s. Another one: --client cursor --print json gives the common mcpServers form.",
				"Клиенты: %s. Для другого клиента: --client cursor --print json даёт общий формат mcpServers.", strings.Join(clientNames(), ", ")))
	}
	srv, err := o.server(c)
	if err != nil {
		return err
	}
	printer := cmdPrinter(cmd)
	format := o.print
	if format == "" && len(c.config) == 0 {
		format = string(c.format) // the client's file is not on this machine
	}
	if format != "" {
		if o.skill {
			return clerrors.New(clerrors.CodeUsage, i18n.T("--skill writes a file, --print writes none", "--skill пишет файл, а --print не пишет ничего"),
				i18n.T("Print the rules with 'evo agent skill'.", "Правила печатает 'evo agent skill'."))
		}
		return printAgentEntry(cmd, printer, c, srv, format)
	}

	scope, err := c.scopeFor(o.scope)
	if err != nil {
		return err
	}
	if srv.spec.url == "" {
		// The client starts evo here, with this machine's key: without one
		// every tool call would fail.
		cfg, err := resolveConfig(printer, map[string]any{})
		if err != nil {
			return err
		}
		if cfg.KeyID == "" || cfg.Secret == "" {
			return auth.ErrNoCredentials()
		}
	}
	entry, placeholder := c.entry(srv.spec)
	var rules *cobra.Command // the tree the rules are built from, with --skill
	if o.skill {
		rules = cmd.Root()
	}
	targets, err := c.targets(scope, entry, rules, srv.readOnly, srv.toolsets)
	if err != nil {
		return err
	}
	changes := make([]agentChange, 0, len(targets))
	for _, t := range targets {
		ch, err := readChange(t)
		if err != nil {
			return err
		}
		changes = append(changes, ch)
	}
	changed, err := applyAgentChanges(cmd, printer, c, scope, changes, o.force)
	if err != nil || flagDryRun && changed {
		return err // --dry-run: the diff is all there is
	}

	if printer.IsJSON() {
		files := make([]map[string]string, len(changes))
		for i, ch := range changes {
			files[i] = map[string]string{"path": ch.path, "status": ch.status()}
		}
		return printer.JSON(map[string]any{"client": c.name, "scope": scope, "files": files})
	}
	for _, ch := range changes {
		printer.Success(ch.done())
	}
	if srv.spec.url == "" {
		if w := pathEvoWarning(); w != "" {
			printer.Warning(w)
		}
	}
	if changed {
		printer.Hint(i18n.Tf("Restart %s or reload its MCP servers to pick up the change.",
			"Перезапустите %s или перечитайте его MCP-серверы, чтобы изменение вступило в силу.", c.title))
	}
	agentHints(printer, c, srv, placeholder)
	return nil
}

// printAgentEntry prints the client's entry in format and writes nothing.
func printAgentEntry(cmd *cobra.Command, printer *output.Printer, c agentClient, srv agentServer, format string) error {
	if format != string(formatJSON) && format != string(formatTOML) {
		return clerrors.New(clerrors.CodeUsage, i18n.Tf("--print %q: json or toml", "--print %q: json или toml", format), "")
	}
	entry, placeholder := c.entry(srv.spec)
	doc := c.render(configFormat(format), entry)
	if printer.IsJSON() {
		// One JSON document on stdout, whatever the format printed.
		return printer.JSON(map[string]string{"client": c.name, "format": format, "config": string(doc)})
	}
	if _, err := cmd.OutOrStdout().Write(doc); err != nil {
		return err
	}
	agentHints(printer, c, srv, placeholder)
	return nil
}

// applyAgentChanges writes the changes that change something, after the
// consent (confirm.Ask: the diff, a prompt, --yes, --dry-run). Another evo
// entry of a client's file is refused without force, with its diff, before
// anything is asked or written; the rules are evo's, a newer text of them
// is no conflict. It reports whether there was anything to
// change (with --dry-run, nothing is written all the same).
func applyAgentChanges(cmd *cobra.Command, printer *output.Printer, c agentClient, scope agentScope, changes []agentChange, force bool) (bool, error) {
	var pending []agentChange
	var lines []string
	var details []map[string]string
	var conflict string
	for _, ch := range changes {
		diff := ch.diff()
		switch {
		case ch.result.State == agentconf.Same:
			continue
		case ch.result.State == agentconf.Replaced && !force && !ch.generated:
			if conflict == "" {
				conflict = ch.path
			}
			if !printer.IsJSON() {
				fmt.Fprintln(cmd.ErrOrStderr(), strings.Join(diff, "\n"))
			}
			continue
		}
		pending = append(pending, ch)
		lines = append(lines, diff...)
		details = append(details, map[string]string{"path": ch.path, "status": ch.status(), "diff": strings.Join(diff, "\n")})
	}
	if conflict != "" {
		return false, clerrors.New(clerrors.CodeUsage,
			i18n.Tf("%s already holds a different evo entry", "в %s уже есть другая запись evo", conflict),
			i18n.T("The diff shows what --force writes: re-run with --force to replace the entry (--dry-run --force to see it again).",
				"diff показывает, что запишет --force: повторите с --force, чтобы заменить запись (--dry-run --force — посмотреть ещё раз)."))
	}
	if len(pending) == 0 {
		return false, nil
	}
	paths := make([]string, len(pending))
	for i, ch := range pending {
		paths[i] = ch.path
	}
	ok, err := confirm.Ask(cmd.Context(), confirm.Preview{
		Action:  "agent setup",
		Summary: i18n.Tf("write the evo MCP server for %s into %s", "записать MCP-сервер evo для %s в %s", c.title, strings.Join(paths, ", ")),
		Lines:   lines,
		Details: map[string]any{"client": c.name, "scope": scope, "files": details},
	}, confirm.Options{Yes: flagYes, DryRun: flagDryRun, JSON: flagJSON, Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()})
	if !ok {
		return true, err
	}
	for _, ch := range pending {
		if err := ch.write(); err != nil {
			return true, err
		}
	}
	return true, nil
}

// targets are the files to write: the client's configuration and, given
// the command tree to build them from, its rules — those of a read-only
// agent for a read-only server — and, where the rules are a skill of their
// own, the cards of the groups the agent sees (toolsets; nil: every group)
// in cards/ next to it (#515, layer B).
func (c agentClient) targets(scope agentScope, e agentconf.Entry, rules *cobra.Command, readOnly bool, toolsets []string) ([]agentTarget, error) {
	path, err := c.config[scope].path()
	if err != nil {
		return nil, err
	}
	out := []agentTarget{{path: path, mode: 0o600, merge: func(old []byte) (agentconf.Result, error) { return c.merge(old, e) }}}
	if rules == nil {
		return out, nil
	}
	sf, ok := c.skill[scope]
	if !ok {
		return nil, clerrors.New(clerrors.CodeUsage,
			i18n.Tf("%s keeps no rules file of scope %s", "у %s нет файла правил уровня %s", c.title, scope),
			i18n.T("Print the rules with 'evo agent skill' and put them where the client reads them.",
				"Напечатайте правила 'evo agent skill' и положите туда, где их читает клиент."))
	}
	if path, err = sf.path(); err != nil {
		return nil, err
	}
	merge := func(old []byte) (agentconf.Result, error) {
		return agentconf.Section(old, agentSkillBody(rules, readOnly))
	}
	if sf.whole {
		merge = func(old []byte) (agentconf.Result, error) {
			return agentconf.Whole(old, []byte(agentSkill(rules, readOnly))), nil
		}
	}
	out = append(out, agentTarget{path: path, mode: 0o644, merge: merge, generated: true})
	if !sf.whole {
		return out, nil
	}
	if toolsets == nil {
		toolsets = tools.Groups()
	}
	for _, g := range toolsets {
		card, ok := tools.GroupCard(g)
		if !ok {
			continue
		}
		out = append(out, agentTarget{path: filepath.Join(filepath.Dir(path), "cards", g+".md"), mode: 0o644, generated: true,
			merge: func(old []byte) (agentconf.Result, error) { return agentconf.Whole(old, []byte(card)), nil }})
	}
	return out, nil
}

// readChange reads the target and puts evo's part in. A UTF-8 byte order
// mark (Notepad, PowerShell's Out-File) is not part of the format: the
// merge reads past it, and it stays in front of the file.
func readChange(t agentTarget) (agentChange, error) {
	old, perm, exists, err := readUserFile(t.path)
	if err != nil {
		return agentChange{}, err
	}
	if !exists {
		perm = t.mode
	}
	body, bom := bytes.CutPrefix(old, []byte(utf8BOM))
	r, err := t.merge(body)
	if err != nil {
		var fe *agentconf.FileError
		if stderrors.As(err, &fe) {
			return agentChange{}, clerrors.Wrap(clerrors.CodeConfigInvalid, t.path+": "+fe.Error(),
				i18n.T("The file is left as it is: fix it, or print the entry with --print and add it by hand.",
					"Файл не изменён: исправьте его или напечатайте запись с --print и добавьте вручную."), err)
		}
		return agentChange{}, err
	}
	if bom {
		r.Data = append([]byte(utf8BOM), r.Data...)
	}
	return agentChange{agentTarget: t, old: old, exists: exists, perm: perm, result: r}, nil
}

// readUserFile reads a file of the user's that evo is about to edit: its
// contents, mode (0 for a new one) and whether it is there. A symbolic
// link is refused: rename would replace the link, and writing through it
// would edit a file somewhere else (a link may come with a repository).
func readUserFile(path string) ([]byte, fs.FileMode, bool, error) {
	st, err := os.Lstat(path)
	switch {
	case stderrors.Is(err, fs.ErrNotExist):
		return nil, 0, false, nil
	case err != nil:
		return nil, 0, false, err
	case st.Mode()&fs.ModeSymlink != 0:
		return nil, 0, false, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("%s is a symbolic link; evo does not write through it", "%s — символическая ссылка; evo не пишет через неё", path),
			i18n.T("Add the entry to the file it points to by hand: --print shows it.",
				"Добавьте запись в файл, на который она указывает, вручную: --print её покажет."))
	case !st.Mode().IsRegular():
		return nil, 0, false, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("%s is not a regular file", "%s — не обычный файл", path), "")
	}
	data, err := os.ReadFile(path)
	return data, st.Mode().Perm(), true, err
}

func (ch agentChange) diff() []string { return agentconf.Diff(ch.path, ch.old, ch.result.Data) }

// status is what happens to the file: created, added (the entry, to a file
// that was there), replaced, unchanged.
func (ch agentChange) status() string {
	switch {
	case ch.result.State == agentconf.Same:
		return "unchanged"
	case ch.result.State == agentconf.Replaced:
		return "replaced"
	case !ch.exists:
		return "created"
	}
	return "added"
}

// done is the line of the result.
func (ch agentChange) done() string {
	switch ch.status() {
	case "unchanged":
		return i18n.Tf("%s: evo is already there, nothing changed", "%s: evo уже есть, ничего не изменилось", ch.path)
	case "replaced":
		return i18n.Tf("%s: evo replaced", "%s: запись evo заменена", ch.path)
	case "created":
		return i18n.Tf("%s: created", "%s: создан", ch.path)
	}
	return i18n.Tf("%s: evo added", "%s: evo добавлен", ch.path)
}

// write replaces the file atomically with its mode, unless it changed
// since it was read: a client may rewrite its file (Claude Code keeps
// ~/.claude.json up to date) while the person reads the diff.
func (ch agentChange) write() error {
	now, _, exists, err := readUserFile(ch.path)
	if err != nil {
		return err
	}
	if exists != ch.exists || !bytes.Equal(now, ch.old) {
		return fmt.Errorf(i18n.T("%s changed while evo waited for the answer, nothing was written to it: run the command again",
			"%s изменился, пока evo ждал ответа, в него ничего не записано: повторите команду"), ch.path)
	}
	return config.WriteFileAtomic(ch.path, ch.result.Data, ch.perm)
}

// pathEvoWarning is the warning about the evo the client starts from its
// PATH, on this machine ("" when it is this evo): none there, or another
// binary — an older evo from install.sh ahead of this one would serve the
// agent silently (live check of #217, 23.10). The client's PATH may differ
// from this shell's; this one is what can be checked.
func pathEvoWarning() string {
	found, err := exec.LookPath(evoCommand)
	if err != nil {
		return i18n.T(`evo is not in PATH: the client will not find it; put evo in PATH or give its full path in "command"`,
			`evo нет в PATH: клиент его не найдёт; добавьте evo в PATH или укажите полный путь в "command"`)
	}
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	a, errA := os.Stat(found)
	b, errB := os.Stat(self)
	if errA != nil || errB != nil || os.SameFile(a, b) {
		return ""
	}
	return i18n.Tf(`evo in PATH is %s, not this evo (%s, version %s): the client will start that one; put this one first in PATH or give its full path in "command"`,
		`evo в PATH — %s, а не этот evo (%s, версия %s): клиент запустит тот; поставьте этот первым в PATH или укажите полный путь в "command"`,
		found, self, version)
}

// agentHints tell what the entry needs beyond the file: the server of an
// HTTP entry and its token, the groups the agent sees, the boundary of a
// read-only server, the client's note.
func agentHints(p *output.Printer, c agentClient, s agentServer, placeholder bool) {
	if s.readOnly {
		p.Warning(i18n.T("--read-only bounds only the MCP server: an agent with a terminal can change the cloud with the evo CLI and the same key; for an agent nobody watches, give its profile a key whose service account has only a viewer role",
			"--read-only ограничивает только сервер MCP: агент с терминалом может изменить облако через evo CLI с тем же ключом; агенту без присмотра дайте профиль с ключом сервисного аккаунта, у которого только роль наблюдателя"))
	}
	if s.spec.url != "" {
		server := append(append(append([]string{"evo"}, profileArgs()...), "mcp", "--http", listenAddress(s.spec.url)), s.serverFlags()...)
		p.Hint(i18n.Tf("The entry reaches a running server: %s", "Запись подключает запущенный сервер: %s", strings.Join(server, " ")))
		if placeholder {
			p.Hint(i18n.Tf("Replace <%s> with the server's token: the client does not read it from the environment.",
				"Замените <%s> токеном сервера: из окружения клиент его не читает.", envBearerToken))
		} else {
			p.Hint(i18n.Tf("The client reads the token from $%s: set it where the client runs.",
				"Клиент берёт токен из $%s: задайте её там, где он запущен.", envBearerToken))
		}
	} else {
		p.Hint(toolsetsHint(s))
	}
	if note := i18n.T(c.noteEN, c.noteRU); note != "" {
		p.Hint(note)
	}
}

// toolsetsHint says what the agent sees and how to change it.
func toolsetsHint(s agentServer) string {
	n := 0
	in := map[string]bool{}
	for _, g := range s.toolsets {
		in[g] = true
	}
	for _, t := range tools.New(nil).Tools() {
		if (s.toolsets == nil || in[t.Group]) && !(s.readOnly && tools.AlwaysNeedsConsent(t)) {
			n++
		}
	}
	if s.toolsets == nil {
		return i18n.Tf("The agent sees every tool group (%d tools); fewer save its context: --toolsets %s.",
			"Агент видит все группы инструментов (%d); меньше — экономия его контекста: --toolsets %s.", n, strings.Join(defaultToolsets, ","))
	}
	more := strings.Join(s.toolsets, ",")
	if !in[exampleGroup] {
		more += "," + exampleGroup
	}
	return i18n.Tf("The agent sees the groups %s (%d tools). More: --toolsets %s; every group: --toolsets all; the groups: 'evo tools list'.",
		"Агент видит группы %s (инструментов: %d). Больше: --toolsets %s; все группы: --toolsets all; список групп — 'evo tools list'.",
		strings.Join(s.toolsets, ","), n, more)
}

// exampleGroup is the group the hint adds to show how to add one.
const exampleGroup = "k8s"

// listenAddress is the --http address of the server behind the URL: its
// host and port when the URL names the server itself (http with a port),
// else a placeholder (https ends at a TLS proxy in front of the server).
func listenAddress(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Scheme == "http" && u.Port() != "" {
		return u.Host
	}
	return "<host:port>"
}
