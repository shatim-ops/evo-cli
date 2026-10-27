// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/journal"
	clog "github.com/shatim-ops/evo-cli/internal/log"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// envBearerToken holds the token clients must present in HTTP mode. It is
// read from the environment only: a flag would show up in ps and in the
// container spec.
const envBearerToken = "MCP_BEARER_TOKEN"

// Authentication modes of the HTTP transport.
const (
	authBearer = "bearer"
	authNone   = "none"
)

func newMCPCmd() *cobra.Command {
	var (
		httpAddr          string
		authMode          string
		allowSecretExport bool
		readOnly          bool
		allowProjects     []string
		toolsets          []string
	)
	cmd := &cobra.Command{
		Use: "mcp",
		Short: i18n.T("Run as an MCP server (stdio, or Streamable HTTP with --http) for AI agents",
			"MCP-сервер для ИИ-агентов (stdio или Streamable HTTP с --http)"),
		Long: i18n.T(mcpHelpEN, mcpHelpRU),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCP(cmd, httpAddr, authMode, allowSecretExport, readOnly, allowProjects, toolsets)
		},
	}
	cmd.Flags().StringVar(&httpAddr, "http", "", i18n.T("Serve MCP over Streamable HTTP on this address (e.g. :8080) instead of stdio",
		"Обслуживать MCP по Streamable HTTP на этом адресе (например, :8080), а не по stdio"))
	cmd.Flags().StringVar(&authMode, "auth", authBearer, i18n.T("HTTP authentication: bearer (token from $MCP_BEARER_TOKEN) or none (loopback address only)",
		"Аутентификация HTTP: bearer (токен из $MCP_BEARER_TOKEN) или none (только адрес loopback)"))
	cmd.Flags().StringSliceVar(&allowProjects, "allow-project", nil, i18n.T("Allow tool calls only in these Cloud.ru projects (UUIDs; repeat or comma-separate). Default: any project the key sees",
		"Разрешить вызовы инструментов только в этих проектах Cloud.ru (UUID; флаг повторяется или через запятую). По умолчанию — любой проект, который видит ключ"))
	cmd.Flags().BoolVar(&allowSecretExport, "allow-secret-export", false, i18n.T("HTTP mode: allow k8s_kubeconfig with an embedded token, cert_private_key, secret_value and unmasked credentials in results",
		"Режим HTTP: разрешить k8s_kubeconfig со встроенным токеном, cert_private_key, secret_value и немаскированные учётные данные в ответах"))
	cmd.Flags().BoolVar(&readOnly, "read-only", false, i18n.T("Make no call that needs the user's consent (changes in the cloud, secrets) and hand out no secret, on either transport: a permission boundary for an agent nobody watches",
		"Не выполнять вызовы, которым нужно согласие человека (изменения в облаке, секреты), и не отдавать секреты — в любом транспорте: граница прав для агента без присмотра"))
	cmd.Flags().StringSliceVar(&toolsets, "toolsets", nil, i18n.T("Advertise only these tool groups (comma-separated, e.g. deploy,vm,vpc; see 'evo tools list'). Default: all. Saves the agent's context; not a permission boundary",
		"Показывать агенту только эти группы инструментов (через запятую, например deploy,vm,vpc; группы — в 'evo tools list'). По умолчанию все. Экономит контекст агента; это не ограничение прав"))
	cmd.AddCommand(newMCPHealthcheckCmd())
	return cmd
}

const mcpHelpEN = `Run evo as a Model Context Protocol server.

The server exposes the evo tools (VMs, disks, networks, Kubernetes,
databases, ...) so an AI agent (e.g. Claude Code) can manage Cloud.ru
resources directly.

By default it speaks newline-delimited JSON-RPC 2.0 on stdin/stdout; all
human-readable output is suppressed (stdout is reserved for the protocol).
SIGINT/SIGTERM and notifications/cancelled cancel running tool calls;
closing stdin does not (calls already sent finish and are answered). Add it to Claude Code with:
  claude mcp add --transport stdio --scope project evo -- evo mcp

With --http ADDR it serves the MCP Streamable HTTP transport on
http://ADDR/mcp (and an unauthenticated liveness probe on /healthz):
  MCP_BEARER_TOKEN=... evo mcp --http :8080
  claude mcp add --transport http evo http://localhost:8080/mcp \
    --header "Authorization: Bearer $MCP_BEARER_TOKEN"

HTTP authentication (--auth):
  bearer  clients send "Authorization: Bearer <token>"; the token is taken
          from $MCP_BEARER_TOKEN, and the server refuses to start without it
  none    no authentication; only allowed on a loopback address
          (--http 127.0.0.1:8080)

Over HTTP, k8s_kubeconfig with an embedded token, cert_private_key and
secret_value are disabled, and credential fields (passwords, keys, tokens) in
other results are masked, unless --allow-secret-export is set. Arguments that
reach the server's host are confined: deploy's compose_file to the working
directory, k8s_kubectl to built-in commands without host flags (--kubeconfig,
-f, ...), k8s_helm to remote charts on public https/oci hosts.
k8s_kubectl/k8s_apply/k8s_helm answer with an error when kubectl/helm is not
installed (the Docker image has neither). At most 32 tool calls run at once
and a batch holds at most 100 messages. A tool call is cancelled when its
client closes the connection, deletes the session (DELETE /mcp) or sends
notifications/cancelled, and when the server stops; there is no timeout. One log line per tool call (name,
status, duration; never the arguments) goes to stderr.

Cloud.ru credentials and project parameters are the server's own, resolved
exactly as for the regular CLI commands (environment, profile from
'evo configure', .evo.yaml); clients never send them.

Projects: deploy and provision accept a project_id argument, so with a key
that sees several projects (an organization-level key) any client of the
server can work in any of them. --allow-project limits the tools to the
listed projects (repeat the flag or separate with commas): a call in any other
project, the server's own included, fails, and so does a call on a resource
the tool names by id (a cluster, a VM, a DNS zone) outside those projects.
The read-only org_quotas and iam_service_accounts still read other
projects. Without the flag the server
warns at start in HTTP mode. Prefer a project-level key for a shared server.

Read-only: --read-only makes no call that needs the user's consent
(confirmed=true: a change in the cloud, secret_value, cert_private_key) and
hands out no secret (no cluster token, no Secret data from kubectl,
credential fields masked), over stdio and HTTP alike. Tools every call of
which needs consent are left out of tools/list; k8s_kubectl and k8s_helm
keep their reads. Use it for an agent nobody watches: such an agent sets
confirmed=true itself, so the gate of a tool does not stop it, and
--toolsets limits only what it is shown.`

const mcpHelpRU = `Запустить evo как сервер Model Context Protocol.

Сервер открывает инструменты evo (ВМ, диски, сети, Kubernetes, базы данных
и другие), и ИИ-агент (например, Claude Code) управляет ресурсами Cloud.ru
напрямую. Описания инструментов и их ответы — на английском: их читает агент.

По умолчанию — JSON-RPC 2.0 построчно через stdin/stdout; текст для человека
не печатается (stdout занят протоколом). SIGINT/SIGTERM и
notifications/cancelled отменяют выполняемые вызовы; закрытие stdin — нет
(уже отправленные вызовы доработают и получат ответ). Подключение к Claude Code:
  claude mcp add --transport stdio --scope project evo -- evo mcp

С --http ADDR сервер работает по MCP Streamable HTTP на http://ADDR/mcp
(и отвечает на проверку живости без аутентификации на /healthz):
  MCP_BEARER_TOKEN=... evo mcp --http :8080
  claude mcp add --transport http evo http://localhost:8080/mcp \
    --header "Authorization: Bearer $MCP_BEARER_TOKEN"

Аутентификация HTTP (--auth):
  bearer  клиент передаёт "Authorization: Bearer <токен>"; токен берётся из
          $MCP_BEARER_TOKEN, без него сервер не запускается
  none    без аутентификации; только на адресе loopback
          (--http 127.0.0.1:8080)

По HTTP отключены k8s_kubeconfig со встроенным токеном, cert_private_key и
secret_value, а учётные данные (пароли, ключи, токены) в остальных ответах
маскируются, если не задан --allow-secret-export. Аргументы, которые
затрагивают хост сервера, ограничены: compose_file у deploy — рабочим
каталогом, k8s_kubectl — встроенными командами без флагов хоста (--kubeconfig,
-f и т. п.), k8s_helm — удалёнными чартами на публичных хостах https/oci.
k8s_kubectl/k8s_apply/k8s_helm отвечают ошибкой, если kubectl/helm не
установлен (в Docker-образе нет ни того, ни другого). Одновременно выполняется
не больше 32 вызовов, в пакете — не больше 100 сообщений. Вызов отменяется,
когда клиент закрывает соединение, удаляет сессию (DELETE /mcp) или
присылает notifications/cancelled, и при остановке сервера; тайм-аута нет. На
каждый вызов в stderr пишется одна строка журнала (имя, статус,
длительность; аргументы — никогда).

Ключ доступа Cloud.ru и параметры проекта — собственные у сервера и
определяются так же, как у обычных команд (переменные окружения, профиль из
'evo configure', .evo.yaml); клиенты их не передают.

Проекты: deploy и provision принимают аргумент project_id, поэтому с ключом,
который видит несколько проектов (ключ уровня организации), любой клиент
сервера может работать в любом из них. --allow-project ограничивает
инструменты перечисленными проектами (флаг повторяется или через запятую):
вызов в любом другом проекте, в том числе в проекте самого сервера,
отклоняется, как и вызов с ресурсом, который инструмент называет по id
(кластер, ВМ, зона DNS), не из этих проектов. org_quotas и
iam_service_accounts только читают и по-прежнему
видят другие проекты. Без флага сервер в режиме HTTP предупреждает при
запуске. Для общего сервера лучше ключ уровня проекта.

Только чтение: с --read-only сервер не выполняет ни одного вызова, которому
нужно согласие (confirmed=true: изменение в облаке, secret_value,
cert_private_key), и не отдаёт секреты (ни токена кластера, ни данных Secret
из kubectl, учётные данные в ответах маскируются) — и по stdio, и по HTTP.
Инструментов, которым согласие нужно всегда, нет в tools/list; у k8s_kubectl
и k8s_helm остаются чтения. Это режим для агента без присмотра: такой агент
сам ставит confirmed=true, и гейт инструмента его не остановит, а --toolsets
ограничивает только то, что ему показано.`

// runMCP validates the transport flags and serves. The process speaks
// English from its first check: the engine's progress captured into tool
// results (deploy's "log") and the server's log are read by agents and
// operators, not by the person at the terminal, and a refusal to start is
// read in the same log (docs/auth.md: evo mcp always answers in English;
// break r4, R4-12).
func runMCP(cmd *cobra.Command, httpAddr, authMode string, allowSecretExport, readOnly bool, allowProjects, toolsets []string) error {
	i18n.SetLang(i18n.EN)
	if readOnly && allowSecretExport {
		return clerrors.New(clerrors.CodeUsage,
			i18n.T("--read-only hands out no secret: it does not go with --allow-secret-export",
				"--read-only не отдаёт секреты: с --allow-secret-export он несовместим"),
			i18n.T("Drop one of the two flags.", "Уберите один из двух флагов."))
	}
	srv := newMCPServer(allowProjects)
	if err := srv.SetToolsets(toolsets); err != nil {
		return clerrors.Wrap(clerrors.CodeUsage, i18n.Tf("--toolsets: %v", "--toolsets: %v", err),
			i18n.T("Groups are listed by 'evo tools list'.", "Группы — в выводе 'evo tools list'."), err)
	}
	if readOnly {
		srv.SetReadOnly()
	}

	if httpAddr == "" {
		if cmd.Flags().Changed("auth") || allowSecretExport {
			return clerrors.New(clerrors.CodeUsage,
				i18n.T("--auth and --allow-secret-export apply to the HTTP transport only",
					"--auth и --allow-secret-export действуют только для транспорта HTTP"),
				i18n.T("Add --http ADDR, e.g. --http 127.0.0.1:8080", "Добавьте --http ADDR, например --http 127.0.0.1:8080"))
		}
		journal.Begin(journal.SurfaceMCPStdio, currentVersion().Version)
		return runMCPStdio(cmd.Context(), srv)
	}

	token, err := httpAuthToken(httpAddr, authMode, os.Getenv(envBearerToken))
	if err != nil {
		return err
	}
	logger := clog.New(cmd.ErrOrStderr(), flagVerbose)
	if w := bearerTokenWarning(token); w != "" {
		logger.Warn(w)
	}
	if len(allowProjects) == 0 {
		logger.Warn("no --allow-project: a client can pass project_id to deploy/provision and work in any project the key sees; " +
			"limit it with --allow-project <UUID> or use a project-level key")
	}
	journal.Begin(journal.SurfaceMCPHTTP, currentVersion().Version)
	return srv.ListenAndServe(cmd.Context(), httpAddr, mcp.HTTPOptions{
		BearerToken:       token,
		AllowSecretExport: allowSecretExport,
		Logger:            logger.Logger,
	}, nil)
}

// newMCPServer is the server of evo mcp on the projects of --allow-project
// (none: any project the key sees). With the flag the tools are bound to
// the server's project (H9): a read that names no project reads that
// project, not the whole billing agreement or organization.
func newMCPServer(allowProjects []string) *mcp.Server {
	srv := mcp.New(mcpEngineFactory(allowProjects), version)
	if len(allowProjects) > 0 {
		srv.SetProjectBound()
	}
	return srv
}

// mcpEngineFactory builds the engine of every tool call. Tools that take a
// project_id argument (deploy, provision) pass it as the ProjectID override,
// so the check sees the project the call resolves to, not only the
// server's own.
func mcpEngineFactory(allowProjects []string) func(map[string]any) (*engine.Engine, error) {
	return func(overrides map[string]any) (*engine.Engine, error) {
		if overrides == nil {
			overrides = map[string]any{}
		}
		// The SSH key stays lazy here too: only the tools that
		// reach a VM call EnsureSSHKey.
		eng, err := buildEngine(overrides)
		if err != nil {
			return nil, err
		}
		// stdout is the JSON-RPC channel in stdio mode: the engine's
		// progress lines (a released floating IP, a FIP attached after
		// boot) go to stderr, the server log, unless a tool captures them
		// in its own buffer (deploy, logs, destroy). They went to stdout
		// from destroy vm_id= and provision (final audit, stage 1).
		eng.Printer = output.NewWithWriter(os.Stderr, output.ModeText, false)
		if err := checkAllowedProject(allowProjects, eng.Config.ProjectID); err != nil {
			return nil, err
		}
		return eng, nil
	}
}

// checkAllowedProject refuses an engine for a project outside --allow-project
// (QA P4). Every tool that reaches the cloud builds its engine through the
// factory, so this covers project_id arguments and the server's own project.
func checkAllowedProject(allowed []string, project string) error {
	if len(allowed) == 0 {
		return nil
	}
	for _, p := range allowed {
		if p == project {
			return nil
		}
	}
	list := strings.Join(allowed, ",")
	shown, hint := project, fmt.Sprintf("The server runs with --allow-project %s; ask its operator to add the project.", list)
	if project == "" {
		shown = "(not set)"
		hint = fmt.Sprintf("The server has no project of its own: pass project_id (one of %s) where the tool takes one.", list)
	}
	return clerrors.Wrap(clerrors.CodeConfigInvalid,
		fmt.Sprintf("project %s is not allowed on this server", shown), hint, tools.ErrProjectNotAllowed)
}

// runMCPStdio serves the stdio transport.
func runMCPStdio(ctx context.Context, srv *mcp.Server) error {
	// SIGINT/SIGTERM (ctx) cancel the running tool call; RunContext
	// returns once it has wound down (the call records what it created),
	// at most a few seconds later. The end of stdin cancels nothing.
	return srv.RunContext(ctx, os.Stdin, os.Stdout)
}

// minBearerTokenLen is the shortest bearer token the HTTP server takes on
// an address open to the network; on loopback a shorter one starts with a
// warning (bearerTokenWarning). 32 random characters, as "openssl rand
// -hex 32" gives 64 (security audit 19.10, SEC-9; D45: refused off
// loopback since 1.3.1).
const minBearerTokenLen = 32

// bearerTokenWarning is the startup warning about a short bearer token, ""
// when there is none (no token with --auth none, or a long enough one).
func bearerTokenWarning(token string) string {
	if token == "" || len(token) >= minBearerTokenLen {
		return ""
	}
	return fmt.Sprintf("%s is shorter than %d characters; use a long random token, e.g. openssl rand -hex 32", envBearerToken, minBearerTokenLen)
}

// httpAuthToken validates the --http/--auth combination and returns the
// bearer token to require ("" for --auth none). The server never listens
// without authentication on a non-loopback address.
func httpAuthToken(addr, mode, envToken string) (string, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", clerrors.Wrap(clerrors.CodeUsage, i18n.Tf("invalid --http address %q", "недопустимый адрес --http %q", addr),
			i18n.T("Use HOST:PORT or :PORT, e.g. --http :8080 or --http 127.0.0.1:8080",
				"Укажите ХОСТ:ПОРТ или :ПОРТ, например --http :8080 или --http 127.0.0.1:8080"), err)
	}
	switch mode {
	case authBearer:
		if envToken == "" {
			return "", clerrors.New(clerrors.CodeConfigInvalid,
				envBearerToken+i18n.T(" is not set: the HTTP server does not start without authentication",
					" не задан: без аутентификации HTTP-сервер не запускается"),
				i18n.Tf("Set %s to a long random string (e.g. openssl rand -hex 32), "+
					"or use --auth none with a loopback address (--http 127.0.0.1:8080)",
					"Задайте в %s длинную случайную строку (например, openssl rand -hex 32) "+
						"или используйте --auth none с адресом loopback (--http 127.0.0.1:8080)", envBearerToken))
		}
		// A short token guards a server every host of the network can
		// reach: it does not start (D45). On loopback only this machine
		// reaches it, and the warning says it.
		if len(envToken) < minBearerTokenLen && !mcp.LoopbackHost(host) {
			return "", clerrors.New(clerrors.CodeConfigInvalid,
				i18n.Tf("%s is shorter than %d characters: the HTTP server on %s does not start with it",
					"%s короче %d символов: HTTP-сервер на %s с ним не запускается", envBearerToken, minBearerTokenLen, addr),
				i18n.Tf("Set %s to a long random string (e.g. openssl rand -hex 32), or listen on a loopback address (--http 127.0.0.1:8080)",
					"Задайте в %s длинную случайную строку (например, openssl rand -hex 32) или слушайте адрес loopback (--http 127.0.0.1:8080)", envBearerToken))
		}
		return envToken, nil
	case authNone:
		if !mcp.LoopbackHost(host) { // an empty host (":8080") is every interface
			return "", clerrors.New(clerrors.CodeUsage,
				i18n.Tf("--auth none is only allowed on a loopback address, not %q",
					"--auth none допускается только на адресе loopback, а не на %q", addr),
				i18n.T("Use --http 127.0.0.1:PORT for a local server, or --auth bearer with ",
					"Для локального сервера — --http 127.0.0.1:ПОРТ, иначе --auth bearer с ")+envBearerToken)
		}
		return "", nil
	default:
		return "", clerrors.New(clerrors.CodeUsage,
			i18n.Tf("unknown --auth %q", "неизвестное значение --auth %q", mode),
			i18n.T("Use --auth bearer or --auth none", "Допустимо --auth bearer или --auth none"))
	}
}

// newMCPHealthcheckCmd is the Docker HEALTHCHECK of the image: the
// distroless base has no shell or curl, so the binary probes itself.
func newMCPHealthcheckCmd() *cobra.Command {
	var url string
	cmd := &cobra.Command{
		Use:    "healthcheck",
		Short:  i18n.T("Probe /healthz of a running HTTP MCP server (exit 0 when healthy)", "Проверить /healthz работающего HTTP MCP-сервера (код 0 — работает)"),
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				url = os.Getenv("MCP_HEALTHCHECK_URL")
			}
			if url == "" {
				url = "http://127.0.0.1:8080/healthz"
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("healthcheck: %w", err)
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("healthcheck: %s returned %s", url, resp.Status)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&url, "url", "", i18n.T("Health endpoint (default $MCP_HEALTHCHECK_URL or http://127.0.0.1:8080/healthz)",
		"Адрес проверки (по умолчанию $MCP_HEALTHCHECK_URL или http://127.0.0.1:8080/healthz)"))
	return cmd
}
