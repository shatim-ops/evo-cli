// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/product"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Build identity, passed to Main by cmd/evo (and the legacy cmd/cloudru-vm),
// whose own variables a release build sets through goreleaser:
//
//	-ldflags "-X main.version=1.2.3 -X main.commit=<sha> -X main.date=<RFC 3339>"
//
// A plain "go build" / "go install" leaves the defaults, and resolveVersion
// fills them from the module and VCS data the Go toolchain embeds.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Global flags.
var (
	flagJSON    bool
	flagVerbose bool
	flagNoColor bool
	flagProfile string
	flagYes     bool
	flagDryRun  bool
)

// httpClient is shared by the IAM and compute clients: one connection pool
// and a timeout, so a stalled Cloud.ru endpoint cannot hang the CLI forever.
var httpClient = &http.Client{Timeout: 60 * time.Second, Transport: httpretry.UserAgent(nil)}

// commandStarted is set once cobra has parsed flags and arguments and is
// about to run a command. An error before that is a usage error.
var commandStarted bool

// Main runs the CLI with the build identity of the calling command and
// exits. The code lives here, not in cmd/evo, so that the legacy build path
// cmd/cloudru-vm ("go build ./cmd/cloudru-vm" in existing scripts) builds
// the same program until v1.1.
func Main(buildVersion, buildCommit, buildDate string) {
	setBuildIdentity(buildVersion, buildCommit, buildDate)
	if isLegacyAlias(os.Args[0]) {
		printAliasNotice(os.Stderr, os.Args[1:])
	}
	os.Exit(run(os.Args[1:]))
}

// aliasNotice is printed when the binary runs under its legacy name: the
// release archives ship a copy named after the legacy alias for one minor
// release, so scripts and MCP configs written for the old name keep working.
const aliasNotice = config.LegacyAppName + " переименован в " + config.AppName + ", alias будет удалён в v1.1"

// isLegacyAlias reports whether argv0 is the legacy alias binary:
// cloudru-vm (legacy name), or cloudru-vm.exe on Windows.
func isLegacyAlias(argv0 string) bool {
	name := filepath.Base(argv0)
	if ext := filepath.Ext(name); strings.EqualFold(ext, ".exe") {
		name = strings.TrimSuffix(name, ext)
	}
	return name == config.LegacyAppName
}

// printAliasNotice writes the one-line rename notice of the legacy alias to
// stderr; with --json as {"warning": ...}, so stderr stays parseable.
func printAliasNotice(w io.Writer, args []string) {
	if jsonRequested(args) {
		_ = json.NewEncoder(w).Encode(map[string]string{"warning": aliasNotice})
		return
	}
	fmt.Fprintln(w, aliasNotice)
}

// run executes the CLI and returns the process exit code.
func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		// After the first signal restore the default behaviour, so a second
		// Ctrl-C kills a command that does not watch the context.
		<-ctx.Done()
		stop()
	}()

	commandStarted = false
	// Until flags are parsed: plain text to stderr.
	config.SetWarnFunc(func(msg string) { output.New(false, true).Warning(msg) })
	if msg, err := config.MigrateLegacyDir(); err != nil {
		output.New(jsonRequested(args), true).Warning(i18n.Tf(
			"could not copy the profiles of %s to %s: %v; run '%s configure' again or copy them by hand",
			"не удалось скопировать профили %s в %s: %v; выполните '%s configure' заново или скопируйте их вручную",
			config.LegacyAppName, config.Dir(), err, config.AppName))
	} else if msg != "" {
		output.New(jsonRequested(args), true).Warning(msg)
	}
	rootCmd := newRootCmd()
	rootCmd.SetArgs(args)
	cmd, err := rootCmd.ExecuteContextC(ctx)
	if err == nil {
		return clerrors.ExitOK
	}
	if ctx.Err() != nil && !stderrors.Is(err, context.Canceled) {
		// The command was interrupted but reported it as something else.
		err = fmt.Errorf("%w: %w", context.Canceled, err)
	}
	if cmd == nil {
		cmd = rootCmd
	}
	if !commandStarted {
		err = usageError(cmd, err)
	}
	// cobra stops at the first bad flag or command, and k8s kubectl passes
	// what follows the cluster id to kubectl, refusing a --json there
	// (break r3b, R3B-3): a --json it never parsed still makes the error JSON.
	flagJSON = flagJSON || jsonRequested(args)
	handleError(output.New(flagJSON, flagNoColor), err)
	return clerrors.ExitCode(err)
}

// jsonRequested reports whether the command line asks for --json, looking
// at the raw arguments up to "--".
func jsonRequested(args []string) bool {
	on := false
	for _, a := range args {
		switch {
		case a == "--":
			return on
		case a == "--json":
			on = true
		case strings.HasPrefix(a, "--json="):
			if b, err := strconv.ParseBool(strings.TrimPrefix(a, "--json=")); err == nil {
				on = b
			}
		}
	}
	return on
}

// usageError turns a cobra parsing error (unknown command or flag, bad
// argument) into a CLIError pointing at --help.
func usageError(cmd *cobra.Command, err error) error {
	return clerrors.Wrap(clerrors.CodeUsage, usageMessage(err.Error()),
		i18n.Tf("Run '%s --help' for usage.", "Справка: '%s --help'.", cmd.CommandPath()), err)
}

// asGroup makes cmd a command group that shows its help when called alone
// and rejects anything else: by default cobra answers "vm lst" with the
// group's help and exit 0, and a script takes the typo for success.
func asGroup(cmd *cobra.Command) *cobra.Command {
	cmd.SuggestionsMinimumDistance = 2 // cobra's default, set only for the root
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		msg := i18n.Tf("unknown command %q for %q", "неизвестная команда %q для %q", args[0], cmd.CommandPath())
		if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
			msg += i18n.T("; did you mean ", "; может быть, ") + strings.Join(s, ", ") + "?"
		}
		return stderrors.New(msg)
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	return cmd
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "evo",
		Short: product.Name + i18n.T(": CLI and MCP server for VMs, networks, k8s, PostgreSQL and deploy of compose projects",
			": CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов"),
		Long: product.Name + " (evo)\n\n" + i18n.T("CLI and MCP server for Cloud.ru Evolution: deploy of docker-compose projects\n"+
			"to VMs and management of cloud resources: VMs, disks, networks, Kubernetes,\n"+
			"PostgreSQL, Object Storage, secrets.",
			"CLI и MCP-сервер для Cloud.ru Evolution: развёртывание docker-compose проектов\n"+
				"на ВМ и управление ресурсами облака: ВМ, диски, сети, Kubernetes, PostgreSQL,\n"+
				"Object Storage, секреты.\n\n"+
				"Сообщения по-английски: EVO_LANG=en.") +
			"\n\n" + i18n.T("Feedback: Telegram ", "Обратная связь: Telegram ") + product.FeedbackContact + i18n.T(", testers' chat ", ", чат тестировщиков ") + product.FeedbackChat,
		// evo --version prints what 'evo version' prints (benchmark: every
		// CLI there has the flag).
		Version: currentVersion().Version,
		// Errors are printed once, by run(): as text or, with --json, as
		// {"error": {...}} on stderr.
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			commandStarted = true
			// Warnings about legacy paths (.cloudru-vm/, .cloudru-vm.yaml)
			// follow --json and --no-color from here on.
			config.SetWarnFunc(func(msg string) {
				printWarnings(output.New(flagJSON, flagNoColor), []string{msg})
			})
			if v := os.Getenv("EVO_LANG"); i18n.Unsupported(v) {
				printWarnings(output.New(flagJSON, flagNoColor), []string{
					fmt.Sprintf("EVO_LANG=%q не поддерживается (en или ru): вывод на русском", v)})
			}
		},
	}

	var versionText strings.Builder
	_ = printVersion(&versionText, false, currentVersion())
	cmd.SetVersionTemplate(versionText.String())
	cmd.Flags().Bool("version", false, i18n.T("Print the version (as 'evo version')", "Версия (как 'evo version')"))
	cmd.PersistentFlags().BoolVar(&flagJSON, "json", false, i18n.T("Output in JSON format", "Вывод в формате JSON"))
	cmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, i18n.T("Enable verbose logging", "Подробный журнал"))
	cmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, i18n.T("Disable colored output", "Без цвета"))
	cmd.PersistentFlags().StringVar(&flagProfile, "profile", "", i18n.T("Credentials profile (default: $CLOUDRU_PROFILE or \"default\")",
		"Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или \"default\")"))
	cmd.PersistentFlags().BoolVarP(&flagYes, "yes", "y", false, i18n.T("Confirm operations that create paid resources or delete data, without a prompt",
		"Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные"))
	cmd.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false, i18n.T("Show what would be created or deleted (with a cost estimate) and exit",
		"Показать, что будет создано или удалено (с оценкой цены), и выйти"))
	addScopeFlags(cmd)

	cmd.AddCommand(
		newDeployCmd(),
		newStatusCmd(),
		newLogsCmd(),
		newDestroyCmd(),
		newVerifyCmd(),
		newInitCmd(),
		newVersionCmd(),
		newCrewCmd(),
		newListZonesCmd(),
		newListImagesCmd(),
		newMCPCmd(),
		newConfigureCmd(),
		newProjectCmd(),
		newWhoamiCmd(),
		newDoctorCmd(),
		newToolsCmd(),
	)
	cmd.AddCommand(newTreeCmds()...)
	applyExamples(cmd)
	groupCommands(cmd)
	localizeHelp(cmd)
	// cobra adds completion on Execute, as a command without a run: it
	// answered "completion tcsh" with its help and exit 0. As a group it
	// refuses an unknown shell (USAGE, exit 1) (release audit, L-13).
	cmd.InitDefaultCompletionCmd()
	for _, c := range cmd.Commands() {
		if c.Name() == "completion" {
			asGroup(c)
		}
	}

	return cmd
}

// buildEngine constructs the Engine from config and global flags.
func buildEngine(flagOverrides map[string]any) (*engine.Engine, error) {
	flagOverrides["JSON"] = flagJSON
	flagOverrides["Verbose"] = flagVerbose
	flagOverrides["NoColor"] = flagNoColor
	applyScopeFlags(flagOverrides)

	printer := output.New(flagJSON, flagNoColor)
	cfg, err := resolveConfig(printer, flagOverrides)
	if err != nil {
		return nil, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf(i18n.T("get working directory: %w", "текущий каталог: %w"), err)
	}

	auth := tokenProvider(cfg)
	cloud := provision.NewCloudRuAdapter(httpClient, "")
	// Every compute request asks the provider for the token instead of
	// using the one taken at the start: a long deploy outlived it (Fable
	// on r3a). The provider renews it before it expires.
	cloud.SetTokenSource(func(ctx context.Context) (string, error) {
		tok, err := auth.Token(ctx)
		return tok.Value, err
	})
	return &engine.Engine{
		Config:   cfg,
		Auth:     auth,
		Cloud:    cloud,
		Deployer: deploy.NewSSHDeployer(),
		Printer:  printer,
		StateDir: cwd,
		// CLOUDRU_TIMEOUT / timeout: how long deploy waits for the VM to
		// become active (default 300 s, the engine's own default).
		Timings: engine.Timings{VMPollTimeout: time.Duration(cfg.Timeout) * time.Second},
		// Created on first use: list-zones and friends leave no key behind.
		SSHKeyLoader: func() ([]byte, string, error) {
			priv, pub, err := loadOrGenerateSSHKey(cwd)
			if err == nil {
				printWarnings(printer, sshKeyWarnings(cwd))
			}
			return priv, pub, err
		},
	}, nil
}

// loadOrGenerateSSHKey loads an existing SSH key pair from .evo/ or
// generates a new one and persists it for future use.
//
// .evo/ may come with someone else's repository. A symlink there
// (the directory itself, or a key file pointing at ~/.ssh/authorized_keys or
// a file another user owns) is refused instead of followed: writing through
// it would overwrite that file with our key, or hand the private key to the
// owner of the target.
func loadOrGenerateSSHKey(projectDir string) (privPEM []byte, pubKey string, err error) {
	keyDir := config.DataDir(projectDir)
	privPath := filepath.Join(keyDir, "id_ed25519")
	pubPath := filepath.Join(keyDir, "id_ed25519.pub")

	if err := ensureKeyDir(keyDir); err != nil {
		return nil, "", err
	}
	ignoreInGit(keyDir)

	// Try loading existing key pair.
	privPEM, privMode, errPriv := readKeyFile(privPath)
	pubBytes, _, errPub := readKeyFile(pubPath)
	for _, e := range []error{errPriv, errPub} {
		if e != nil && !os.IsNotExist(e) {
			return nil, "", e
		}
	}
	if errPriv == nil && errPub == nil && len(privPEM) > 0 && len(pubBytes) > 0 {
		if err := checkPrivateKeyMode(privPath, privMode); err != nil {
			return nil, "", err
		}
		return privPEM, string(pubBytes), nil
	}

	// Generate new key pair.
	privPEM, pubKey, err = auth.GenerateSSHKeyPair()
	if err != nil {
		return nil, "", err
	}

	// Persist to disk: temporary file 0600 + rename, so the key is never
	// readable by others and an existing file or symlink lends nothing.
	if err := config.WriteFilePrivate(privPath, privPEM); err != nil {
		return nil, "", fmt.Errorf(i18n.T("write private key: %w", "запись закрытого ключа: %w"), err)
	}
	if err := config.WriteFileAtomic(pubPath, []byte(pubKey), 0o644); err != nil {
		return nil, "", fmt.Errorf(i18n.T("write public key: %w", "запись открытого ключа: %w"), err)
	}

	return privPEM, pubKey, nil
}

// ensureKeyDir creates the key directory 0700, or checks that the existing
// one is a real directory and tightens it to 0700.
func ensureKeyDir(dir string) error {
	st, err := os.Lstat(dir)
	switch {
	case os.IsNotExist(err):
		if err := config.MkdirPrivate(dir); err != nil {
			return fmt.Errorf(i18n.T("create key directory: %w", "создание каталога ключа: %w"), err)
		}
		return nil
	case err != nil:
		return fmt.Errorf(i18n.T("key directory: %w", "каталог ключа: %w"), err)
	case !st.IsDir():
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("%s is not a directory (a symlink?); refusing to keep the SSH key there",
				"%s — не каталог (символическая ссылка?); хранить в нём SSH-ключ нельзя", dir),
			i18n.T("Remove it (it may have come with the repository) and run the command again",
				"Удалите его (он мог прийти вместе с репозиторием) и повторите команду"))
	}
	if config.PermissionsEnforced() && config.LooseMode(st.Mode()) {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf(i18n.T("restrict key directory: %w", "ограничение прав каталога ключа: %w"), err)
		}
	}
	// Windows: a directory made by an older version inherited the ACL of
	// the project folder. Best effort (FAT has no ACL to set): what stays
	// open is reported by sshKeyWarnings.
	if !config.PermissionsEnforced() && len(config.ForeignAccess(dir, true)) > 0 {
		_ = config.RestrictToOwner(dir, true)
	}
	return nil
}

// sshKeyWarnings reports a key directory or private key other users can
// reach. Only Windows ACLs are checked: on Unix ensureKeyDir makes the
// directory 0700, the key is written 0600 and a looser existing key is
// refused (checkPrivateKeyMode).
func sshKeyWarnings(projectDir string) []string {
	keyDir := config.DataDir(projectDir)
	var out []string
	for _, w := range []string{
		config.PrivateDirWarning(keyDir),
		config.PrivateFileWarning(filepath.Join(keyDir, "id_ed25519")),
	} {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// ignoreInGit puts a .gitignore with "*" into dir, so "git add ." in the
// project never picks up the private key, whatever the project's own
// .gitignore says. Best effort; an existing file (or symlink) is left alone.
func ignoreInGit(dir string) {
	f, err := os.OpenFile(filepath.Join(dir, ".gitignore"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString("*\n")
	_ = f.Close()
}

// readKeyFile reads a key file that must be a regular file, not a symlink,
// and returns its mode for checkPrivateKeyMode.
func readKeyFile(path string) ([]byte, os.FileMode, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !st.Mode().IsRegular() {
		return nil, 0, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("%s is not a regular file (a symlink?); refusing to use it as the SSH key",
				"%s — не обычный файл (символическая ссылка?); как SSH-ключ он не используется", path),
			i18n.T("Remove it (it may have come with the repository) and run the command again: a new key is created",
				"Удалите его (он мог прийти вместе с репозиторием) и повторите команду: будет создан новый ключ"))
	}
	data, err := os.ReadFile(path)
	return data, st.Mode(), err
}

// checkPrivateKeyMode refuses, as OpenSSH does ("UNPROTECTED PRIVATE KEY
// FILE"), a private key that group or others can access. evo writes the key
// 0600, so a looser one did not come from evo: most likely it was committed
// to a template repository and arrived with the clone (0644), and using it
// would put the template author's public key into the new VM's
// authorized_keys. Windows has no such mode bits: there the ACL is reported
// by sshKeyWarnings. Every path to the deploy key, CLI and MCP alike
// (deploy, deploy_plan, deploy_apply, logs), loads it through the engine's
// SSHKeyLoader, that is through loadOrGenerateSSHKey.
func checkPrivateKeyMode(path string, mode os.FileMode) error {
	if !config.PermissionsEnforced() || !config.LooseMode(mode) {
		return nil
	}
	return clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("UNPROTECTED PRIVATE KEY FILE: %s has permissions %04o and is accessible by group or others; refusing to use it",
			"НЕЗАЩИЩЁННЫЙ ЗАКРЫТЫЙ КЛЮЧ: у %s права %04o, он доступен группе или другим пользователям; использовать его нельзя",
			path, mode.Perm()),
		i18n.Tf("evo creates the key with mode 0600, so this one may have come with the repository: remove %s and run the command again (a new key is created). If the key is your own, run: chmod 600 %s",
			"evo создаёт ключ с правами 0600, поэтому этот мог прийти вместе с репозиторием: удалите %s и повторите команду (будет создан новый ключ). Если ключ ваш, выполните: chmod 600 %s",
			filepath.Dir(path), path))
}

// projectConfigPath is the project file with per-project overrides. User
// settings and secrets live in the profile (see 'evo configure').
func projectConfigPath() string {
	return config.ProjectFile(".")
}

// handleError prints err to the printer's error writer: as text with the
// hint of the CLIError found anywhere in the wrap chain, or, in JSON mode,
// as {"error": {"code": N, "reason": "CODE", "message": "...", "hint": "..."}}.
func handleError(p *output.Printer, err error) {
	code := clerrors.ExitCode(err)
	var message, hint string
	var cliErr *clerrors.CLIError
	switch {
	case code == clerrors.ExitInterrupted:
		message = i18n.T("interrupted", "прервано")
		if stderrors.As(err, &cliErr) {
			// e.g. deploy: which VM was left running and how to delete it.
			message += ": " + cliErr.Message
			hint = cliErr.Hint
		}
	case stderrors.As(err, &cliErr):
		hint = cliErr.Hint
		if err == error(cliErr) {
			message = cliErr.Message
		} else {
			// Keep the context added by fmt.Errorf("...: %w").
			message = err.Error()
		}
	default:
		message = err.Error()
	}

	if p.IsJSON() {
		_ = p.JSONError(code, clerrors.Reason(err), message, hint)
		return
	}
	p.Errorf("%s", message)
	// --verbose: the underlying cause too (SSH exit status, HTTP error),
	// which the message and hint summarise (QA r4 Q14).
	if flagVerbose && cliErr != nil && cliErr.Err != nil {
		p.Errorf(i18n.T("cause: %s", "причина: %s"), cliErr.Err)
	}
	if hint != "" {
		p.Hint(hint)
	}
}

// --- deploy ---

func newDeployCmd() *cobra.Command {
	var (
		composeFile    string
		flavor         string
		region         string
		projectID      string
		imageID        string
		image          string
		diskSize       int
		force          bool
		nonInteractive bool
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Args:  cobra.NoArgs,
		Short: i18n.T("Deploy a docker-compose project to a Cloud.ru VM", "Развернуть docker-compose проект на ВМ Cloud.ru"),
		Long: i18n.T(`Deploy a docker-compose project to a Cloud.ru VM.

The first run creates a VM with a floating IP, opens the compose file's ports
in the zone's security group, installs Docker, uploads the compose file and
runs 'docker compose up -d'. Before the VM is created it shows a preview with
the price (the Cloud.ru price calculation, else evo's estimate) and asks for
confirmation: a question on the terminal, --yes to agree in advance,
--dry-run for the preview only. A repeated run updates the same VM if the
compose file changed (with --force, even if it did not).`, `Развернуть docker-compose проект на ВМ Cloud.ru.

Первый запуск создаёт ВМ с floating IP, открывает порты из compose-файла в
группе безопасности зоны, ставит Docker, загружает compose-файл и выполняет
'docker compose up -d'. Перед созданием ВМ показывается превью с ценой
(расчёт Cloud.ru, иначе оценка evo) и запрашивается подтверждение: вопрос на
терминале, --yes — согласие заранее, --dry-run — только превью. Повторный
запуск обновляет ту же ВМ, если compose-файл изменился (с --force — и без
изменений).`),
		Example: i18n.T(`  evo deploy --dry-run
  evo deploy --yes
  evo deploy -f compose.prod.yml --flavor lowcost10-2-4`, `  evo deploy --dry-run
  evo deploy --yes
  evo deploy -f compose.prod.yml --flavor lowcost10-2-4`),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := map[string]any{}
			if cmd.Flags().Changed("compose-file") {
				flags["ComposeFile"] = composeFile
			}
			if cmd.Flags().Changed("flavor") {
				flags["Flavor"] = flavor
			}
			if cmd.Flags().Changed("region") {
				flags["Region"] = region
			}
			if cmd.Flags().Changed("project-id") {
				flags["ProjectID"] = projectID
			}
			if cmd.Flags().Changed("image-id") && cmd.Flags().Changed("image") {
				return clerrors.New(clerrors.CodeUsage,
					i18n.T("--image and --image-id name the same thing", "--image и --image-id задают одно и то же"),
					i18n.T("Keep one: --image takes a name or a UUID.", "Оставьте один: --image принимает имя или UUID."))
			}
			if cmd.Flags().Changed("image-id") {
				flags["ImageID"] = imageID
			}
			if cmd.Flags().Changed("image") {
				flags["Image"] = image
			}
			if cmd.Flags().Changed("disk-size") {
				flags["DiskSize"] = diskSize
			}

			eng, err := buildEngine(flags)
			if err != nil {
				return err
			}

			opts := engine.DeployOptions{
				ComposeFile:    eng.Config.ComposeFile,
				Flavor:         eng.Config.Flavor,
				Region:         eng.Config.Region,
				Force:          force,
				NonInteractive: nonInteractive,
				Yes:            flagYes,
				DryRun:         flagDryRun,
			}

			if err := eng.Deploy(cmd.Context(), opts); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&composeFile, "compose-file", "f", "", i18n.T("Path to docker-compose file (default: docker-compose.yml)",
		"Путь к compose-файлу (по умолчанию docker-compose.yml)"))
	cmd.Flags().StringVar(&flavor, "flavor", "", i18n.T("VM flavor (default: auto)", "Flavor ВМ (по умолчанию auto — подбор по сервисам)"))
	cmd.Flags().StringVar(&region, "region", "", i18n.T("Availability zone UUID", "UUID зоны доступности"))
	cmd.Flags().StringVar(&projectID, "project-id", "", i18n.T("Cloud.ru project UUID", "UUID проекта Cloud.ru"))
	cmd.Flags().StringVar(&imageID, "image-id", "", i18n.T("Boot image UUID", "UUID загрузочного образа"))
	cmd.Flags().StringVar(&image, "image", "", i18n.T("Boot image: a name from 'evo image list' or a UUID (Ubuntu or Debian)",
		"Загрузочный образ: имя из 'evo image list' или UUID (Ubuntu или Debian)"))
	cmd.Flags().IntVar(&diskSize, "disk-size", 0, i18n.T("Boot disk size in GB (default: 10)", "Размер загрузочного диска, ГБ (по умолчанию 10)"))
	cmd.Flags().BoolVar(&force, "force", false, i18n.T("Force redeploy even if compose hash unchanged",
		"Развернуть заново, даже если compose-файл не изменился"))
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, i18n.T("Never prompt; creating a VM then requires --yes",
		"Никогда не спрашивать; тогда для создания ВМ нужен --yes"))

	return cmd
}

// --- status ---

func newStatusCmd() *cobra.Command {
	var noRefresh bool
	cmd := &cobra.Command{
		Use:   "status",
		Args:  cobra.NoArgs,
		Short: i18n.T("Show current deployment status", "Состояние текущего развёртывания"),
		Long: i18n.T(`Show this directory's deployment from the local state, and the VM's live
state from one request to Cloud.ru (at most 8 s). A VM deleted outside evo
(in the console) shows as not found in the cloud: 'evo destroy' removes its
record. If the cloud cannot be asked (no key, network, API error), status
still shows the local state and says why the cloud was not checked. status
never changes the state.

--no-refresh: the local state only, no request. With --json the answer adds
cloud_checked, cloud_state, not_found_in_cloud, cloud_error and cloud_reason.`,
			`Показать развёртывание этого каталога по локальному state и живое состояние
ВМ — одним запросом к Cloud.ru (не дольше 8 с). ВМ, удалённая вне evo (в
консоли), показывается как не найденная в облаке: 'evo destroy' уберёт её
запись. Если облако спросить не удалось (нет ключа, сети, ошибка API),
status всё равно показывает локальное состояние и пишет, почему облако не
проверено. state команда не меняет.

--no-refresh — только локальный state, без запроса. С --json в ответе
добавляются cloud_checked, cloud_state, not_found_in_cloud, cloud_error и
cloud_reason.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}
			return eng.Status(cmd.Context(), engine.StatusOptions{NoRefresh: noRefresh})
		},
	}
	cmd.Flags().BoolVar(&noRefresh, "no-refresh", false, i18n.T("Local state only: do not ask Cloud.ru for the VM's live state",
		"Только локальный state: не спрашивать Cloud.ru о живом состоянии ВМ"))
	return cmd
}

// --- logs ---

func newLogsCmd() *cobra.Command {
	var (
		service string
		tail    int
		follow  bool
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Args:  cobra.NoArgs,
		Short: i18n.T("View container logs from the deployed VM", "Журналы контейнеров на развёрнутой ВМ"),
		Example: i18n.T(`  evo logs
  evo logs -s web -n 50
  evo logs -f`, `  evo logs
  evo logs -s web -n 50
  evo logs -f`),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}

			opts := engine.LogsOptions{
				Service: service,
				Tail:    tail,
				Follow:  follow,
			}

			if err := eng.Logs(cmd.Context(), opts); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&service, "service", "s", "", i18n.T("Filter logs by service name", "Только журнал этого сервиса"))
	cmd.Flags().IntVarP(&tail, "tail", "n", 100, i18n.T("Number of lines to show (0 = all)", "Сколько последних строк показать (0 — все)"))
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, i18n.T("Follow log output", "Следить за журналом (до Ctrl-C)"))

	return cmd
}

// --- destroy ---

func newDestroyCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "destroy",
		Args:  cobra.NoArgs,
		Short: i18n.T("Delete the deployed VM and clean up resources", "Удалить развёрнутую ВМ и связанные ресурсы"),
		Long: i18n.T(`Delete this directory's deployment VM, release its floating IP and
remove the security group rules deploy created.

Before deleting it shows a preview and asks for confirmation: a question on
the terminal, --yes to agree in advance, --dry-run for the preview only. Data
on the VM's disks is lost.`, `Удалить ВМ развёртывания из этого каталога, освободить её floating IP и
удалить правила группы безопасности, которые создал deploy.

Перед удалением показывается превью и запрашивается подтверждение: вопрос на
терминале, --yes — согласие заранее, --dry-run — только превью.
Данные на дисках ВМ теряются.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if force {
				cmdPrinter(cmd).Warning(i18n.T("destroy --force is deprecated: use --yes",
					"destroy --force устарел: используйте --yes"))
			}
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}

			opts := engine.DestroyOptions{Force: force || flagYes, DryRun: flagDryRun}
			if err := eng.Destroy(cmd.Context(), opts); err != nil {
				return err
			}
			return nil
		},
	}

	// --force meant the same as --yes here, while deploy --force redeploys
	// an unchanged project: one word, two meanings. It keeps working for old
	// scripts, hidden from help, with a warning.
	cmd.Flags().BoolVar(&force, "force", false, i18n.T("Deprecated: use --yes", "Устарел: используйте --yes"))
	_ = cmd.Flags().MarkHidden("force")

	return cmd
}

// --- verify ---

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:  "verify",
		Args: cobra.NoArgs,
		Short: i18n.T("Run health checks against the deployed application (exit 1 if unhealthy, 4 if nothing is deployed)",
			"Проверить, отвечает ли приложение (код 1 — не отвечает, 4 — ничего не развёрнуто)"),
		Long: i18n.T(`Run health checks against the deployed application: a TCP connect to each
port the compose file publishes, then HTTP (or HTTPS). An HTTP answer 2xx or
3xx is healthy, and so is a port that accepts connections but does not speak
HTTP (a database, a cache).

With --json: an array with one object per port, keys port, status_code (0
without an HTTP answer), healthy, latency_ms, error (only when the check
failed) and protocol ("http", "https", "tcp", or "" when nothing answered).`,
			`Проверить развёрнутое приложение: TCP-соединение с каждым портом, который
публикует compose-файл, затем HTTP (или HTTPS). Ответ HTTP 2xx или 3xx —
работает; порт, который принимает соединения, но не говорит по HTTP (база
данных, кэш), — тоже.

С --json — массив, по объекту на порт, с ключами port, status_code (0 без
ответа HTTP), healthy, latency_ms, error (только если проверка не прошла) и
protocol ("http", "https", "tcp" или "", если ничего не ответило).`),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}
			return eng.Verify(cmd.Context())
		},
	}
}

// --- init ---

func newInitCmd() *cobra.Command {
	var (
		nonInteractive bool
		image          string
	)

	cmd := &cobra.Command{
		Use:  "init",
		Args: cobra.NoArgs,
		Short: i18n.T("Create .evo.yaml with a zone and image chosen from the Cloud.ru API",
			"Создать .evo.yaml с зоной и образом из API Cloud.ru"),
		Long: i18n.T(`Create .evo.yaml for this project.

The availability zone and the boot image are listed from the Cloud.ru API
(credentials required). On a terminal you choose from a numbered list.
Zones where the project has a subnet come first (deploy cannot plug a VM in
elsewhere; a new project has one only in ru.AZ-1): ru.AZ-2 if it has one (on
15.10.2026 ru.AZ-1 failed to create VMs), else the zone of the project's
main subnet. With --non-interactive or without a terminal the first zone and
the newest plain Ubuntu (else Debian) image are taken, and the choice is
printed. --image takes the image by name (any case) or UUID
instead: a name is written to .evo.yaml as the catalogue spells it (image:),
a UUID as image_id:. deploy needs Ubuntu or Debian: Astra Linux, Alt and
RED OS images are refused (create a plain VM from them: evo vm create).`, `Создать .evo.yaml для этого проекта.

Зона доступности и загрузочный образ берутся из API Cloud.ru (нужен ключ
доступа). На терминале их выбирают из нумерованного списка. Первыми идут
зоны, где у проекта есть подсеть (в другую deploy ВМ не подключит; в новом
проекте подсеть есть только в ru.AZ-1): ru.AZ-2, если подсеть есть в ней
(15.10.2026 в ru.AZ-1 ВМ не создавались), иначе зона основной подсети
проекта. С --non-interactive или без терминала берутся первая зона и самый
новый обычный образ Ubuntu (если его нет — Debian), выбор печатается. --image задаёт образ по имени (регистр не важен) или UUID: имя
пишется в .evo.yaml так, как в каталоге (image:), UUID — как image_id:.
deploy нужен Ubuntu или Debian: образы Astra Linux, Альт и РЕД ОС
отклоняются (ВМ из них создаёт evo vm create).`),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}
			return eng.Init(cmd.Context(), engine.InitOptions{
				Path:           ".evo.yaml",
				NonInteractive: nonInteractive,
				Image:          image,
			})
		},
	}

	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, i18n.T("Take the first zone (ru.AZ-2 if the project has a subnet there) and the preferred image without asking",
		"Без вопросов взять первую зону (ru.AZ-2, если в ней есть подсеть проекта) и предпочтительный образ"))
	cmd.Flags().StringVar(&image, "image", "", i18n.T("Boot image by name or UUID (evo image list) instead of the newest Ubuntu",
		"Загрузочный образ по имени или UUID (evo image list) вместо самого нового Ubuntu"))

	return cmd
}

// --- version ---

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:  "version",
		Args: cobra.NoArgs,
		Short: i18n.T("Print the version, commit, build date, Go version and platform",
			"Версия, коммит, дата сборки, версия Go и платформа"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printVersion(cmd.OutOrStdout(), flagJSON, currentVersion())
		},
	}
}

// --- list-zones ---

func newListZonesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-zones",
		Args:  cobra.NoArgs,
		Short: i18n.T("List available availability zones", "Список зон доступности"),
		Long: i18n.T(`List availability zones from the Cloud.ru API.

Use the zone ID as the --region flag or CLOUDRU_REGION env var for deploy.
Use --json for machine-readable output.`, `Список зон доступности из API Cloud.ru.

ID зоны передаётся в deploy флагом --region или переменной CLOUDRU_REGION.
Для машинного разбора — --json.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			tok, err := eng.Auth.Token(ctx)
			if err != nil {
				return err
			}
			eng.Cloud.SetToken(tok.Value)

			zones, err := eng.Cloud.ListAvailabilityZones(ctx)
			if err != nil {
				return err
			}

			if eng.Printer.IsJSON() {
				return eng.Printer.JSON(zones)
			}

			headers := []string{"ID", i18n.T("Name", "Имя"), i18n.T("New resources", "Новые ресурсы")}
			rows := make([][]string, len(zones))
			for i, z := range zones {
				state := i18n.T("open", "открыта")
				if z.Closed() {
					state = i18n.T("closed (enabled=false)", "закрыта (enabled=false)")
				}
				rows[i] = []string{z.ID, z.Name, state}
			}
			eng.Printer.Table(headers, rows)
			return nil
		},
	}
}

// --- list-images ---

// list-images is the old form of 'evo image list' (review r5 U6): hidden,
// kept working, with a one-line hint.
func newListImagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "list-images",
		Hidden: true,
		Args:   cobra.NoArgs,
		Short:  i18n.T("List available VM boot images", "Список загрузочных образов ВМ"),
		Long: i18n.T(`List boot images from the Cloud.ru API.

Use the image ID as the --image-id flag or CLOUDRU_IMAGE_ID env var for deploy.
deploy installs Docker on Ubuntu and Debian images (e.g. Ubuntu-24.04).
Use --json for machine-readable output.`, `Список загрузочных образов из API Cloud.ru.

ID образа передаётся в deploy флагом --image-id или переменной CLOUDRU_IMAGE_ID.
deploy ставит Docker на образы Ubuntu и Debian (например, Ubuntu-24.04).
Для машинного разбора — --json.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdPrinter(cmd).Warning(i18n.T("evo list-images is deprecated: use 'evo image list [query]'",
				"evo list-images устарела: используйте 'evo image list [запрос]'"))
			eng, err := toolEngine(map[string]any{})
			if err != nil {
				return err
			}
			eng.Printer = cmdPrinter(cmd)

			if err := eng.ListImages(cmd.Context()); err != nil {
				return err
			}
			return nil
		},
	}
}
