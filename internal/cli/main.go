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
	"github.com/shatim-ops/evo-cli/internal/journal"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/product"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/tools"
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
// the same program until v2.0.
func Main(buildVersion, buildCommit, buildDate string) {
	setBuildIdentity(buildVersion, buildCommit, buildDate)
	if isLegacyAlias(os.Args[0]) {
		printAliasNotice(os.Stderr, os.Args[1:])
	}
	os.Exit(run(os.Args[1:]))
}

// aliasNotice is printed when the binary runs under its legacy name: the
// release archives ship a copy named after the legacy alias until
// v2.0, so scripts and MCP configs written for the old name keep working.
const aliasNotice = config.LegacyAppName + " переименован в " + config.AppName + ", alias будет удалён в v2.0"

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
	// The global flags live for this run: a later engine built without a
	// run (tests, the parity worlds) must not read its --profile,
	// --project-id or --region.
	defer func() {
		flagJSON, flagVerbose, flagNoColor, flagYes, flagDryRun = false, false, false, false, false
		flagProfile, flagProjectID, flagRegion = "", "", ""
	}()
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
	journal.Begin(journal.SurfaceCLI, currentVersion().Version)
	rootCmd := newRootCmd()
	rootCmd.SetArgs(args)
	if mcpCommand(rootCmd, args) {
		// evo mcp answers in English (docs/auth.md), and cobra parses its
		// flags before runMCP sets the language: "--auth" without a value
		// was refused in the CLI's language (release audit, REL-12).
		i18n.SetLang(i18n.EN)
	}
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
	if !commandStarted && cmd == rootCmd {
		// "evo --bogus vm list": cobra strips a flag it does not know as
		// if it took a value, loses "vm" and answers "unknown command
		// "list"" (QA-17). The unknown flag is the error.
		if ferr := leadingUnknownFlag(rootCmd, args); ferr != nil {
			err = ferr
		}
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

// mcpCommand reports whether args run the server, "evo mcp …", and not
// its help: the help of evo mcp stays in the CLI's language, as every
// command's help.
func mcpCommand(root *cobra.Command, args []string) bool {
	c, _, err := root.Find(args)
	if err != nil || c.Name() != "mcp" || c.Parent() != root {
		return false
	}
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "-h" || a == "--help" || strings.HasPrefix(a, "--help=") {
			return false
		}
	}
	return true
}

// leadingUnknownFlag returns the error pflag gives for the first flag
// before the command name that the root does not know, or nil. Cobra finds
// the command with the root's flags stripped, and a flag it does not know is
// taken to have a value: the next argument, the command name, went with it.
func leadingUnknownFlag(root *cobra.Command, args []string) error {
	root.InitDefaultHelpFlag()
	root.InitDefaultVersionFlag()
	local, persistent := root.Flags(), root.PersistentFlags()
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" || a == "-" || !strings.HasPrefix(a, "-") {
			return nil
		}
		if strings.HasPrefix(a, "--") {
			name, _, inline := strings.Cut(a[2:], "=")
			f := local.Lookup(name)
			if f == nil {
				f = persistent.Lookup(name)
			}
			if f == nil {
				return fmt.Errorf("unknown flag: --%s", name)
			}
			if !inline && f.NoOptDefVal == "" {
				i++ // its value
			}
			continue
		}
		// -yv: shorthands; one with a value takes the rest or the next.
		for j := 1; j < len(a); j++ {
			f := local.ShorthandLookup(a[j : j+1])
			if f == nil {
				f = persistent.ShorthandLookup(a[j : j+1])
			}
			if f == nil {
				return fmt.Errorf("unknown shorthand flag: %q in %s", a[j], a)
			}
			if f.NoOptDefVal == "" {
				if j == len(a)-1 {
					i++
				}
				break
			}
		}
	}
	return nil
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
	cmd.Args = unknownSubcommand
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	return cmd
}

// unknownSubcommand is the Args of a command with subcommands: an argument
// that is none of them is "unknown command", with the subcommands it may
// have meant.
func unknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	msg := i18n.Tf("unknown command %q for %q", "неизвестная команда %q для %q", args[0], cmd.CommandPath())
	if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
		msg += i18n.T("; did you mean ", "; может быть, ") + strings.Join(s, ", ") + "?"
	}
	return stderrors.New(msg)
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "evo",
		Short: product.Name + i18n.T(": CLI and MCP server for VMs, networks, k8s, PostgreSQL and deploy of compose projects",
			": CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов"),
		Long: i18n.T(product.TitleEN, product.TitleRU) + "\n\n" + i18n.T("Deploy of docker-compose projects to VMs and management of cloud resources:\n"+
			"VMs, disks, networks, Kubernetes, PostgreSQL, Object Storage, secrets.",
			"Развёртывание docker-compose проектов на ВМ и управление ресурсами облака:\n"+
				"ВМ, диски, сети, Kubernetes, PostgreSQL, Object Storage, секреты.\n\n"+
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
		newAgentCmd(),
		newDiagCmd(),
	)
	cmd.AddCommand(newTreeCmds(cmd)...)
	registerEngineCompletions(cmd)
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
	// doctor shows the configuration's warnings as its checks.
	shown, _ := flagOverrides[tools.WarningsShown].(bool)
	delete(flagOverrides, tools.WarningsShown)
	flagOverrides["JSON"] = flagJSON
	flagOverrides["Verbose"] = flagVerbose
	flagOverrides["NoColor"] = flagNoColor
	applyScopeFlags(flagOverrides)

	printer := output.New(flagJSON, flagNoColor)
	cfg, err := configOf(flagOverrides)
	if err != nil {
		return nil, err
	}
	if !shown {
		printWarnings(printer, cfg.Warnings)
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
		// CLOUDRU_TIMEOUT / timeout: how long deploy and provision wait
		// for the VM to become active (default config.DefaultTimeout, the
		// engine's own default).
		Timings: engine.Timings{VMPollTimeout: time.Duration(cfg.Timeout) * time.Second},
		// The preview of a VM says whether the project's quotas were
		// checked (D60).
		QuotaProbe: tools.QuotaProbe,
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
// owner of the target. So is a key file with more than one hard link.
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

// readKeyFile reads a key file that must be a regular file with one link,
// not a symlink or a hard link, and returns its mode for checkPrivateKeyMode.
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
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	fst, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !os.SameFile(st, fst) {
		// Replaced between Lstat and Open: the file opened is not the one
		// checked above.
		return nil, 0, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("%s changed while it was being read; refusing to use it as the SSH key",
				"%s изменился во время чтения; как SSH-ключ он не используется", path),
			i18n.T("Run the command again", "Повторите команду"))
	}
	// A hard link is a regular file for Lstat, yet the same data is reachable
	// by another path, which may be another user's file or someone else's key
	// planted next to the project (break r4, R4-9). evo writes the key with
	// rename, one link only.
	if n := linkCount(f, fst); n > 1 {
		return nil, 0, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("%s has %d hard links: the same file is reachable by another path; refusing to use it as the SSH key",
				"у %s несколько жёстких ссылок (%d): тот же файл доступен по другому пути; как SSH-ключ он не используется", path, n),
			i18n.T("Remove it (it may have come from someone else) and run the command again: a new key is created. To reuse your own key, copy it (cp) instead of linking it",
				"Удалите его (он мог появиться не от вас) и повторите команду: будет создан новый ключ. Чтобы взять свой ключ, скопируйте его (cp), а не ссылайтесь на него"))
	}
	data, err := io.ReadAll(f)
	return data, fst.Mode(), err
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
// as {"error": {"code": N, "reason": "CODE", "message": "...", "hint": "...",
// "details": {...}}}. The details of a tool's error answer are all in the
// JSON; the text adds the one a person cannot get otherwise, the password
// evo generated for a VM the call left coming up (FB121-1).
func handleError(p *output.Printer, err error) {
	code := clerrors.ExitCode(err)
	var message, hint string
	var details map[string]any
	var cliErr *clerrors.CLIError
	if stderrors.As(err, &cliErr) {
		details = cliErr.Details
	}
	switch {
	case code == clerrors.ExitInterrupted:
		message = i18n.T("interrupted", "прервано")
		// A request the interruption cut off fails as API_UNREACHABLE
		// ("IAM unreachable", a hint about the proxy and "interrupt signal
		// received"): that is Ctrl-C, not the network (QA-16).
		if stderrors.As(err, &cliErr) && cliErr.Code != clerrors.CodeAPIUnreachable {
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
		_ = p.JSONError(code, clerrors.Reason(err), message, hint, details)
		return
	}
	p.Errorf("%s", message)
	// The other refusals of the call, found with the first (a tool's
	// refusals): each on a line of its own; the hint below covers them.
	if refusals, _ := details["refusals"].([]any); len(refusals) > 1 {
		for _, r := range refusals[1:] {
			if m, _ := r.(map[string]any); m != nil {
				if e, _ := m["error"].(string); e != "" {
					p.Errorf("%s", e)
				}
			}
		}
	}
	if pw, _ := details["password"].(string); pw != "" {
		p.Errorf("%s", vmPasswordLine(pw))
	}
	// --verbose: the underlying cause too (SSH exit status, HTTP error),
	// which the message and hint summarise (QA r4 Q14).
	if flagVerbose && cliErr != nil && cliErr.Err != nil {
		p.Errorf(i18n.T("cause: %s", "причина: %s"), cliErr.Err)
	}
	if hint != "" {
		p.Hint(hint)
	}
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
