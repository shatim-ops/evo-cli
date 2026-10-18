package cli

import (
	"bufio"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// orgProjectsURL lists the projects visible to the service account.
var orgProjectsURL = "https://organization.api.cloud.ru/v3/projects"

// Terminal access, replaceable in tests.
var (
	stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	readPassword    = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }
	// saveTerminal returns a function that puts the terminal back as it is
	// now (echo on), for when Ctrl-C abandons a ReadPassword.
	saveTerminal = func() func() {
		fd := int(os.Stdin.Fd())
		st, err := term.GetState(fd)
		if err != nil {
			return func() {}
		}
		return func() { _ = term.Restore(fd, st) }
	}
)

const configureHelp = `Store a service account access key and a default project in a profile.

Interactively asks for the key ID and the secret (not echoed), checks them
against Cloud.ru IAM, then lists the projects the key can see and lets you
pick one (a single project is picked automatically).

Non-interactive (CI, scripts) — the secret is read from stdin, never from a flag:
  printf '%s' "$SECRET" | evo configure --key-id KEY_ID --secret-stdin --project-id PROJECT_ID

Files (mode 0600, directory 0700; CLOUDRU_CONFIG_DIR overrides the directory):
  <user config dir>/evo/config.yaml        project_id, region, defaults
  <user config dir>/evo/credentials.yaml   key_id, secret, optional s3 section

Required access: the service account needs a project role that allows managing
Compute and VPC resources; listing projects also needs read access to the
organization (otherwise pass --project-id). Details:
https://github.com/shatim-ops/evo-cli/blob/main/docs/auth.md`

const configureHelpRU = `Сохранить ключ доступа сервисного аккаунта и проект по умолчанию в профиле.

В интерактивном режиме спрашивает Key ID и секрет (секрет не отображается),
проверяет их в IAM Cloud.ru, затем показывает проекты, которые видит ключ, и
предлагает выбрать один (единственный проект выбирается сам).

Без вопросов (CI, скрипты) — секрет читается из stdin, никогда из флага:
  printf '%s' "$SECRET" | evo configure --key-id KEY_ID --secret-stdin --project-id PROJECT_ID

Файлы (права 0600, каталог 0700; каталог меняет CLOUDRU_CONFIG_DIR):
  <каталог настроек>/evo/config.yaml        project_id, region, умолчания
  <каталог настроек>/evo/credentials.yaml   key_id, secret, необязательная секция s3

Нужные права: сервисному аккаунту нужна роль в проекте, разрешающая управлять
ресурсами Compute и VPC; для списка проектов — ещё и чтение организации
(иначе передайте --project-id). Подробнее:
https://github.com/shatim-ops/evo-cli/blob/main/docs/auth.md`

func newConfigureCmd() *cobra.Command {
	var opts configureOptions
	cmd := &cobra.Command{
		Use:   "configure",
		Short: i18n.T("Set up credentials and the default project for a profile", "Ключ доступа и проект по умолчанию для профиля"),
		Long:  i18n.T(configureHelp, configureHelpRU),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			con := newConsole(os.Stdin, printer.ErrWriter(), !opts.secretStdin && stdinIsTerminal())
			return runConfigure(cmd.Context(), opts, con, printer)
		},
	}
	cmd.Flags().StringVar(&opts.keyID, "key-id", "", i18n.T("Access key ID of the service account", "Key ID ключа доступа сервисного аккаунта"))
	cmd.Flags().BoolVar(&opts.secretStdin, "secret-stdin", false, i18n.T("Read the secret from the first line of stdin (non-interactive)",
		"Прочитать секрет из первой строки stdin (без вопросов)"))
	cmd.Flags().StringVar(&opts.projectID, "project-id", "", i18n.T("Cloud.ru project UUID (skips the project list)",
		"UUID проекта Cloud.ru (без выбора из списка)"))
	cmd.Flags().StringVar(&opts.region, "region", "", i18n.T("Default availability zone UUID for the profile",
		"UUID зоны доступности по умолчанию для профиля"))

	cmd.AddCommand(newConfigureListCmd(), newConfigureDeleteCmd())
	return cmd
}

type configureOptions struct {
	keyID       string
	secretStdin bool
	projectID   string
	region      string
}

// console reads answers from the user; prompts go to stderr.
type console struct {
	in          *bufio.Reader
	out         io.Writer
	interactive bool
}

func newConsole(in io.Reader, out io.Writer, interactive bool) *console {
	return &console{in: bufio.NewReader(in), out: out, interactive: interactive}
}

// readLine reads one trimmed line, giving up when ctx is cancelled: the
// CLI catches SIGINT, so a read that ignores ctx makes Ctrl-C do nothing.
func (c *console) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := c.in.ReadString('\n')
		if err != nil && (err != io.EOF || line == "") {
			ch <- result{"", err}
			return
		}
		ch <- result{strings.TrimSpace(line), nil}
	}()
	select {
	case r := <-ch:
		return r.line, r.err
	case <-ctx.Done():
		fmt.Fprintln(c.out)
		return "", ctx.Err()
	}
}

func (c *console) ask(ctx context.Context, prompt string) (string, error) {
	fmt.Fprint(c.out, prompt)
	return c.readLine(ctx)
}

func (c *console) askSecret(ctx context.Context, prompt string) (string, error) {
	fmt.Fprint(c.out, prompt)
	restore := saveTerminal()
	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	read := readPassword // the goroutine may outlive this call
	go func() {
		b, err := read()
		ch <- result{b, err}
	}()
	select {
	case r := <-ch:
		fmt.Fprintln(c.out)
		if r.err != nil {
			return "", r.err
		}
		return strings.TrimSpace(string(r.b)), nil
	case <-ctx.Done():
		// ReadPassword turned echo off and will never restore it: the
		// process exits first.
		restore()
		fmt.Fprintln(c.out)
		return "", ctx.Err()
	}
}

func runConfigure(ctx context.Context, opts configureOptions, con *console, printer *output.Printer) error {
	name, _ := config.ProfileName(flagProfile)
	if err := config.ValidateProfileName(name); err != nil {
		return err
	}
	prof, creds, _, err := config.LoadProfile(name)
	if err != nil {
		return err
	}
	if con.interactive {
		fmt.Fprintf(con.out, i18n.T("Configuring profile %q in %s\n", "Настройка профиля %q в %s\n"), name, config.Dir())
	}
	// No terminal and no flag at all: nothing to change. With a stored key
	// this used to re-save the profile unchanged and answer «Профиль
	// сохранён» with exit 0 — in CI a forgotten --secret-stdin went
	// unnoticed (QA r4 Q11). --project-id or --region alone still update
	// just them with the stored key.
	if !con.interactive && !opts.secretStdin && opts.keyID == "" && opts.projectID == "" && opts.region == "" {
		return errNeedKey()
	}
	// Before the key is asked for: a word typed for the project ID was
	// saved and every later command failed on it (first real run, v1.0.1).
	if opts.projectID != "" && !looksLikeUUID(opts.projectID) {
		return errNotProjectUUID(opts.projectID)
	}

	// Key ID.
	keyID := opts.keyID
	if keyID == "" && con.interactive {
		prompt := i18n.T("Access key ID: ", "Key ID: ")
		if creds.KeyID != "" {
			prompt = i18n.Tf("Access key ID [%s]: ", "Key ID [%s]: ", config.MaskKeyID(creds.KeyID))
		}
		if keyID, err = con.ask(ctx, prompt); err != nil {
			return fmt.Errorf(i18n.T("read key ID: %w", "чтение Key ID: %w"), err)
		}
	}
	if keyID == "" {
		keyID = creds.KeyID
	}

	// Secret.
	var secret string
	switch {
	case opts.secretStdin:
		if secret, err = con.readLine(ctx); err != nil {
			if ctx.Err() != nil {
				return err
			}
			return clerrors.Wrap(clerrors.CodeConfigInvalid, i18n.T("no secret on stdin", "в stdin нет секрета"),
				i18n.T("Pipe the secret into 'evo configure --secret-stdin'", "Подайте секрет на stdin: printf '%s' \"$SECRET\" | evo configure --secret-stdin"), err)
		}
	case con.interactive:
		prompt := i18n.T("Secret: ", "Секрет: ")
		if creds.Secret != "" && keyID == creds.KeyID {
			prompt = i18n.T("Secret [keep current]: ", "Секрет [Enter — оставить текущий]: ")
		}
		if secret, err = con.askSecret(ctx, prompt); err != nil {
			return fmt.Errorf(i18n.T("read secret: %w", "чтение секрета: %w"), err)
		}
	}
	if secret == "" && keyID == creds.KeyID {
		secret = creds.Secret
	}
	if keyID == "" || secret == "" {
		return errNeedKey()
	}

	// Check the key with IAM itself: a cached token of the same key ID
	// proves nothing about this secret. Keep the token for the next commands.
	provider := auth.NewIAMProvider(auth.Credentials{KeyID: keyID, Secret: secret}, httpClient, nil)
	tok, err := provider.Token(ctx)
	if err != nil {
		return keyCheckError(err)
	}
	_ = auth.FileCache{Path: config.TokenCachePath(name)}.Store(auth.CacheKey(auth.Credentials{KeyID: keyID, Secret: secret}), tok)

	projectID := opts.projectID
	if projectID == "" {
		if projectID, err = chooseProject(ctx, tok.Value, prof.ProjectID, con, printer); err != nil {
			return err
		}
	}

	prof.ProjectID = projectID
	if opts.region != "" {
		prof.Region = opts.region
	}
	creds.KeyID, creds.Secret = keyID, secret
	if err := config.SaveProfile(name, prof, creds); err != nil {
		return err
	}

	if printer.IsJSON() {
		return printer.JSON(map[string]any{
			"profile":    name,
			"key_id":     config.MaskKeyID(keyID),
			"project_id": projectID,
			"config_dir": config.Dir(),
		})
	}
	printer.Success(i18n.Tf("Profile %q saved to %s", "Профиль %q сохранён в %s", name, config.Dir()))
	if name != config.DefaultProfile {
		printer.Hint(i18n.Tf("Use it with --profile %s or CLOUDRU_PROFILE=%s", "Выбирается флагом --profile %s или CLOUDRU_PROFILE=%s", name, name))
	}
	return nil
}

// errNeedKey is the error of a configure without a key to store.
func errNeedKey() error {
	return clerrors.New(clerrors.CodeConfigInvalid, i18n.T("key ID and secret are required", "нужны Key ID и секрет"),
		i18n.T("Run 'evo configure' in a terminal, or pass --key-id and --secret-stdin (--project-id or --region alone change only them)",
			"Запустите 'evo configure' в терминале или передайте --key-id и --secret-stdin (--project-id или --region без них меняют только проект или зону)"))
}

type orgProject struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// listProjects calls the Organization API with an IAM token.
func listProjects(ctx context.Context, token string) ([]orgProject, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, orgProjectsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, clerrors.Wrap(clerrors.CodeAPIUnreachable, i18n.T("failed to reach the Organization API", "Organization API недоступен"),
			i18n.T("Check your network connection, or pass --project-id", "Проверьте сеть или передайте --project-id"), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		code := clerrors.CodeAPIError
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			code = clerrors.CodeAuthFailed
		}
		msg := i18n.Tf("listing projects failed with status %d", "не удалось получить список проектов: статус %d", resp.StatusCode)
		var apiErr struct {
			Message string `json:"message"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&apiErr) == nil && apiErr.Message != "" {
			msg += ": " + apiErr.Message
		}
		// Observed 2026-10-14: for a service account the API answers
		// 400 "(user_id) is invalid argument: user not found".
		return nil, clerrors.New(code, msg,
			i18n.T("The Organization API may not list projects for this key; pass --project-id (find it in the Cloud.ru console)",
				"Organization API может не отдавать список проектов для этого ключа; передайте --project-id (он есть в консоли Cloud.ru)"))
	}
	var body struct {
		Projects []orgProject `json:"projects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, clerrors.Wrap(clerrors.CodeAPIError, i18n.T("unexpected response from the Organization API", "неожиданный ответ Organization API"),
			i18n.T("Pass --project-id", "Передайте --project-id"), err)
	}
	return body.Projects, nil
}

// chooseProject picks the project for the profile: the only one, the
// user's choice, or the current one when it is still in the list.
func chooseProject(ctx context.Context, token, current string, con *console, printer *output.Printer) (string, error) {
	projects, err := listProjects(ctx, token)
	if err != nil {
		if !con.interactive {
			if current != "" {
				// Re-running configure to rotate the key keeps the project.
				printer.Warning(i18n.Tf("%s; keeping project %s", "%s; проект остаётся прежним: %s", projectListText(err), current))
				verboseCause(printer, err)
				return current, nil
			}
			return "", err
		}
		fmt.Fprintln(con.out, projectListText(err)+i18n.T(
			". Enter the project UUID: it is in the Cloud.ru console, in the project details.",
			". Введите UUID проекта: он в консоли Cloud.ru, в сведениях о проекте."))
		verboseCause(printer, err)
		return askProjectID(ctx, con, current)
	}

	switch {
	case len(projects) == 1:
		p := projects[0]
		if !printer.IsJSON() {
			fmt.Fprintf(con.out, i18n.T("Using project %s (%s)\n", "Проект: %s (%s)\n"), p.Name, p.ID)
		}
		return p.ID, nil
	case len(projects) == 0:
		if !con.interactive {
			return "", clerrors.New(clerrors.CodeConfigInvalid, i18n.T("the key sees no projects", "ключ не видит ни одного проекта"),
				i18n.T("Pass --project-id, or grant the service account access to a project",
					"Передайте --project-id или выдайте сервисному аккаунту доступ к проекту"))
		}
		fmt.Fprintln(con.out, i18n.T("The key sees no projects.", "Ключ не видит ни одного проекта."))
		return askProjectID(ctx, con, current)
	}

	if !con.interactive {
		for _, p := range projects {
			if p.ID == current {
				return current, nil
			}
		}
		ids := make([]string, len(projects))
		for i, p := range projects {
			ids[i] = p.ID + " (" + p.Name + ")"
		}
		return "", clerrors.New(clerrors.CodeConfigInvalid, i18n.T("several projects available, none selected",
			"доступно несколько проектов, ни один не выбран"),
			i18n.T("Pass --project-id, one of: ", "Передайте --project-id, один из: ")+strings.Join(ids, ", "))
	}

	def := 0
	fmt.Fprintln(con.out, i18n.T("Projects:", "Проекты:"))
	for i, p := range projects {
		mark := " "
		if p.ID == current {
			mark, def = "*", i+1
		}
		fmt.Fprintf(con.out, " %s%2d) %s  %s\n", mark, i+1, p.ID, p.Name)
	}
	for {
		prompt := i18n.T("Choose a project (number or ID): ", "Выберите проект (номер или ID): ")
		if def > 0 {
			prompt = i18n.Tf("Choose a project (number or ID) [%d]: ", "Выберите проект (номер или ID) [%d]: ", def)
		}
		ans, err := con.ask(ctx, prompt)
		if err != nil {
			return "", fmt.Errorf(i18n.T("read project: %w", "чтение проекта: %w"), err)
		}
		if ans == "" && def > 0 {
			return projects[def-1].ID, nil
		}
		if n, err := strconv.Atoi(ans); err == nil && n >= 1 && n <= len(projects) {
			return projects[n-1].ID, nil
		}
		for _, p := range projects {
			if ans == p.ID {
				return p.ID, nil
			}
		}
		fmt.Fprintln(con.out, i18n.T("Not in the list, try again.", "Такого нет в списке, попробуйте ещё раз."))
	}
}

// projectListText says calmly why there is no project list. The key is
// already checked by then: a refusal of the Organization API is how Cloud.ru
// answers a project's service account (400 "user not found"), not a broken
// key, and the raw error scared the first real user (v1.0.1). The error
// itself is shown with --verbose.
func projectListText(err error) string {
	if clerrors.Reason(err) == clerrors.CodeAPIUnreachable {
		return i18n.T("The project list could not be fetched: the Organization API is unreachable",
			"Список проектов не получен: Organization API недоступен")
	}
	return i18n.T("The project list is not available to this key — this is how Cloud.ru answers a project's service account",
		"Список проектов этому ключу недоступен — так Cloud.ru отвечает сервисным аккаунтам проекта")
}

// verboseCause prints the error behind a calm message with --verbose.
func verboseCause(printer *output.Printer, err error) {
	if flagVerbose {
		printer.Hint(i18n.Tf("cause: %v", "причина: %v", err))
	}
}

// errNotProjectUUID refuses a project ID that is not a UUID.
func errNotProjectUUID(id string) error {
	return clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("project ID %q is not a UUID", "ID проекта %q — не UUID", output.SanitizeCell(id)),
		notProjectUUIDHint())
}

func notProjectUUIDHint() string {
	return i18n.T("A project ID looks like 3f2a8c1e-0b6d-4c8e-9a1f-2d3c4b5a6978: find it in the Cloud.ru console, in the project details. 'evo project use' also takes a project name.",
		"ID проекта выглядит так: 3f2a8c1e-0b6d-4c8e-9a1f-2d3c4b5a6978 — он в консоли Cloud.ru, в сведениях о проекте. 'evo project use' принимает и имя проекта.")
}

// askProjectID asks for the project UUID until it gets one; Enter keeps
// the current project.
func askProjectID(ctx context.Context, con *console, current string) (string, error) {
	prompt := i18n.T("Project ID: ", "ID проекта: ")
	if current != "" {
		prompt = i18n.Tf("Project ID [%s]: ", "ID проекта [%s]: ", current)
	}
	for {
		id, err := con.ask(ctx, prompt)
		if err != nil {
			return "", fmt.Errorf(i18n.T("read project ID: %w", "чтение ID проекта: %w"), err)
		}
		if id == "" {
			id = current
		}
		if id == "" {
			return "", clerrors.New(clerrors.CodeConfigInvalid, i18n.T("project ID is required", "нужен ID проекта"),
				i18n.T("Find it in the Cloud.ru console, or pass --project-id", "Он есть в консоли Cloud.ru; можно передать --project-id"))
		}
		if id == current || looksLikeUUID(id) {
			return id, nil
		}
		fmt.Fprintln(con.out, i18n.T("This is not a project UUID. ", "Это не UUID проекта. ")+notProjectUUIDHint())
	}
}

func newConfigureListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: i18n.T("List stored profiles", "Сохранённые профили"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			profiles, err := config.Profiles()
			if err != nil {
				return err
			}
			active, _ := config.ProfileName(flagProfile)
			if printer.IsJSON() {
				return printer.JSON(map[string]any{"profiles": profiles, "active": active, "config_dir": config.Dir()})
			}
			if len(profiles) == 0 {
				printer.Success(i18n.T("No profiles in ", "Нет профилей в ") + config.Dir())
				printer.Hint(i18n.T("Run 'evo configure' to create one.", "Создать профиль: 'evo configure'."))
				return nil
			}
			rows := make([][]string, len(profiles))
			for i, p := range profiles {
				name := p.Name
				if name == active {
					name += " *"
				}
				rows[i] = []string{name, p.ProjectID, p.KeyID, yesNo(p.HasSecret), yesNo(p.HasS3)}
			}
			printer.Table([]string{i18n.T("Profile", "Профиль"), i18n.T("Project ID", "Проект"), "Key ID", i18n.T("Secret", "Секрет"), "S3"}, rows)
			return nil
		},
	}
}

func newConfigureDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <profile>",
		Short: i18n.T("Delete a stored profile and its cached token", "Удалить сохранённый профиль и его токен из кэша"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			if err := config.DeleteProfile(args[0]); err != nil {
				return err
			}
			if printer.IsJSON() {
				return printer.JSON(map[string]any{"deleted": args[0]})
			}
			printer.Success(i18n.Tf("Profile %q deleted", "Профиль %q удалён", args[0]))
			return nil
		},
	}
}

func yesNo(b bool) string {
	if b {
		return i18n.T("yes", "да")
	}
	return i18n.T("no", "нет")
}

// keyCheckError adds "checking the access key" to an IAM error. A CLIError
// keeps its code and hint, and the code stays out of the text: the message
// read "проверка ключа доступа: AUTH_FAILED: …" (release audit, R-15).
func keyCheckError(err error) error {
	prefix := i18n.T("checking the access key: ", "проверка ключа доступа: ")
	var ce *clerrors.CLIError
	if stderrors.As(err, &ce) {
		return clerrors.Wrap(ce.Code, prefix+ce.Message, ce.Hint, err)
	}
	return fmt.Errorf("%s%w", prefix, err)
}
