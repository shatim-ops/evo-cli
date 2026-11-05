// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/login"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// loginConfig is the login's Config; the tests point it at the world of
// B-2 (internal/loginworld).
var loginConfig = login.Default

// loginSay tells the address to open on stderr: two lines of text, or one
// JSON line with --json (stdout keeps the one answer).
func loginSay(cmd *cobra.Command) func(message, url string) {
	return func(message, u string) {
		if flagJSON {
			_ = json.NewEncoder(cmd.ErrOrStderr()).Encode(map[string]string{"message": message, "url": u})
			return
		}
		fmt.Fprintln(cmd.ErrOrStderr(), message)
		fmt.Fprintln(cmd.ErrOrStderr(), u)
	}
}

// openBrowser opens a URL in the person's browser; the tests replace it.
var openBrowser = func(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}

func newLoginCmd() *cobra.Command {
	var project, forWhom, ttl string
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: i18n.T("Sign in to Cloud.ru in a browser and keep an access key in the profile", "Войти в Cloud.ru через браузер и записать ключ доступа в профиль"),
		Long: i18n.T(`Sign in to Cloud.ru in a browser and grant access on the page evo serves
on this machine: the project, whom the key is for (you: account evo,
profile of now; an agent: account evo-agent, profile «agent») and its
term (7, 30 or 90 days). A terminal, when there is one, asks whom and the
term before the browser; access is granted only by «Grant access» on the
page (no --yes). With your rights evo makes or reuses the service
account, grants it the role «project administrator» until the key's term,
creates the key and keeps it in the profile. The role includes deleting
resources. A previous key evo login made for that profile is revoked.
Your tokens stay in memory; the key's secret is shown nowhere. A refusal
names its step and HTTP status. Over SSH or in a container the sign-in
does not complete. Undo: evo logout.`, `Войти в Cloud.ru через браузер и выдать доступ на странице, которую evo
открывает на этой машине: проект, для кого ключ (вам — аккаунт evo,
текущий профиль; агенту — аккаунт evo-agent, профиль «agent») и его срок
(7, 30 или 90 дней). Терминал, если он есть, спрашивает «кому» и срок до
браузера; доступ выдаёт только кнопка «Выдать доступ» на странице (--yes
нет). Вашими правами evo создаёт или переиспользует сервисный аккаунт,
выдаёт ему роль «Администратор проекта» до срока ключа, создаёт ключ и
записывает его в профиль. Роль включает удаление ресурсов. Прежний ключ
evo login этого профиля отзывается. Ваши токены остаются в памяти; секрет
ключа нигде не показывается. Отказ называет шаг и HTTP-статус. По SSH и в
контейнере вход не пройдёт. Отменить — evo logout.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flagYes {
				return login.RefuseYes()
			}
			choice, err := loginChoice(project, forWhom, ttl)
			if err != nil {
				return err
			}
			store := loginStore{}
			if name, explicit := config.ProfileName(flagProfile); explicit {
				if err := config.ValidateProfileName(name); err != nil {
					return err
				}
				store.name = name
			}
			cfg := loginConfig(loginSay(cmd), openBrowser)
			if noBrowser {
				cfg.Open = nil
			}
			cfg.Tell = loginTell(cmd)
			if !flagJSON && stdinIsTerminal() {
				cfg.Ask = newConsole(os.Stdin, cmd.ErrOrStderr(), true).ask
			}
			res, err := login.Login(cmd.Context(), cfg, choice, store)
			if err != nil {
				return err
			}
			printer := output.New(flagJSON, flagNoColor)
			var revoked any
			if res.RevokedKeyID != "" {
				revoked = config.MaskKeyID(res.RevokedKeyID)
			}
			out := map[string]any{
				"profile": res.Profile, "key_id": config.MaskKeyID(res.KeyID), "project_id": res.ProjectID,
				"project_name": res.ProjectName, "service_account": res.Account, "role": res.Role,
				"key_expires_at": res.KeyExpiresAt.UTC().Format(time.RFC3339), "reused_account": res.AccountReused,
				"revoked_key_id": revoked,
			}
			if res.PreviousNote != "" {
				out["warning"] = res.PreviousNote
			}
			if printer.IsJSON() {
				return printer.JSON(out)
			}
			printer.Success(i18n.Tf("Signed in: the key %s of %s for the project %s is kept in the profile %q until %s",
				"Вход выполнен: ключ %s аккаунта %s для проекта %s записан в профиль %q до %s",
				config.MaskKeyID(res.KeyID), res.Account, res.ProjectName, res.Profile, res.KeyExpiresAt.Local().Format("02.01.2006")))
			if res.PreviousNote != "" {
				printer.Warning(res.PreviousNote)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", i18n.T("Project id or name to offer alone", "id или имя проекта — будет предложен только он"))
	cmd.Flags().StringVar(&forWhom, "for", "", i18n.T("Whom the key is for: me (account evo, profile of now) or agent (account evo-agent, profile «agent»)",
		"Для кого ключ: me (аккаунт evo, текущий профиль) или agent (аккаунт evo-agent, профиль «agent»)"))
	cmd.Flags().StringVar(&ttl, "key-ttl", "", i18n.T("Term of the key: 7d, 30d or 90d (30d by default)", "Срок ключа: 7d, 30d или 90d (по умолчанию 30d)"))
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, i18n.T("Do not open a browser: only print the address (to open on this same machine)", "Не открывать браузер: только напечатать адрес (открыть на этой же машине)"))
	return cmd
}

// agentProfile is the profile of a key for an agent, unless --profile or
// CLOUDRU_PROFILE names one.
const agentProfile = "agent"

// loginStore keeps the key of evo login in a profile: the one named, or
// the one of the audience.
type loginStore struct{ name string }

func (s loginStore) Profile(a login.Audience) string {
	switch {
	case s.name != "":
		return s.name
	case a == login.ForAgent:
		return agentProfile
	}
	return config.DefaultProfile
}

func (s loginStore) Previous(a login.Audience) *login.Previous {
	_, creds, _, err := config.LoadProfile(s.Profile(a))
	if err != nil || creds.Login == nil {
		return nil
	}
	return &login.Previous{ServiceAccountID: creds.Login.ServiceAccountID, AccessKeyID: creds.Login.AccessKeyID, KeyID: creds.KeyID}
}

func (s loginStore) Save(a login.Audience, k login.Saved) error {
	name := s.Profile(a)
	prof, creds, _, err := config.LoadProfile(name)
	if err != nil {
		return err
	}
	prof.ProjectID = k.ProjectID
	creds.KeyID, creds.Secret = k.KeyID, k.Secret
	creds.Login = &config.LoginKey{ServiceAccountID: k.ServiceAccountID, AccessKeyID: k.AccessKeyID,
		KeyExpiresAt: k.KeyExpiresAt.UTC().Format(time.RFC3339), CustomerID: k.CustomerID, Role: k.Role,
		LoggedInAs: k.Login, LoginAt: k.LoginAt.UTC().Format(time.RFC3339)}
	return config.SaveProfile(name, prof, creds)
}

// loginTell writes a line of the login on stderr: text, or one JSON line
// with --json.
func loginTell(cmd *cobra.Command) func(string) {
	return func(line string) {
		if flagJSON {
			_ = json.NewEncoder(cmd.ErrOrStderr()).Encode(map[string]string{"message": line})
			return
		}
		fmt.Fprintln(cmd.ErrOrStderr(), line)
	}
}

// loginChoice is the choice of the flags; a bad one is USAGE before any
// request.
func loginChoice(project, forWhom, ttl string) (login.Choice, error) {
	c := login.Choice{Project: strings.TrimSpace(project), For: login.Audience(forWhom)}
	if forWhom != "" && c.For != login.ForMe && c.For != login.ForAgent {
		return c, clerrors.New(clerrors.CodeUsage, i18n.Tf("--for %q: me or agent", "--for %q: me или agent", forWhom), "")
	}
	if ttl != "" {
		n, err := strconv.Atoi(strings.TrimSuffix(ttl, "d"))
		ok := err == nil
		if ok {
			ok = false
			for _, d := range login.Terms {
				ok = ok || d == n
			}
		}
		if !ok {
			return c, clerrors.New(clerrors.CodeUsage, i18n.Tf("--key-ttl %q: 7d, 30d or 90d", "--key-ttl %q: 7d, 30d или 90d", ttl),
				i18n.T("A key without a term is created in the console only.", "Бессрочный ключ создаётся только в консоли."))
		}
		c.Days = n
	}
	return c, nil
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: i18n.T("Revoke the access key evo login made and delete the profile", "Отозвать ключ, созданный evo login, и удалить профиль"),
		Long: i18n.T(`Delete the access key evo login made for the profile — by its id, with
its own token — then the profile and its cached token. The service
account and its role stay. A profile whose key came from evo configure or
the console is deleted alone: that key stays in the cloud.`, `Удалить ключ доступа, созданный evo login для профиля, — по его id, его же
токеном, — затем профиль и его токен в кэше. Сервисный аккаунт и роль
остаются. Профиль с ключом из evo configure или консоли удаляется один:
этот ключ остаётся в облаке.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := config.ProfileName(flagProfile)
			_, creds, found, err := config.LoadProfile(name)
			if err != nil {
				return err
			}
			if !found {
				return clerrors.New(clerrors.CodeConfigInvalid, i18n.Tf("no profile %q", "профиля %q нет", name),
					i18n.T("The profiles: 'evo configure list'.", "Профили: 'evo configure list'."))
			}
			printer := output.New(flagJSON, flagNoColor)
			summary := i18n.Tf("delete the profile %q", "удалить профиль %q", name)
			if creds.Login != nil {
				summary = i18n.Tf("revoke the access key %s and delete the profile %q", "отозвать ключ доступа %s и удалить профиль %q",
					config.MaskKeyID(creds.KeyID), name)
			}
			ok, err := confirm.Ask(cmd.Context(), confirm.Preview{Action: "logout", Summary: summary,
				Details: map[string]any{"profile": name, "key_id": config.MaskKeyID(creds.KeyID)}},
				confirm.Options{Yes: flagYes, JSON: flagJSON, Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()})
			if !ok {
				return err
			}
			out := map[string]any{"profile": name}
			if creds.Login != nil {
				provider := auth.NewIAMProvider(auth.Credentials{KeyID: creds.KeyID, Secret: creds.Secret}, httpClient, nil)
				status, err := login.Logout(cmd.Context(), loginConfig(loginSay(cmd), openBrowser).AdminBase, provider,
					creds.Login.ServiceAccountID, creds.Login.AccessKeyID)
				if err != nil {
					return err
				}
				out["key"] = status
			} else {
				out["key"] = "kept"
				out["note"] = i18n.T("the key of this profile did not come from evo login: it stays in the cloud — delete it in the console",
					"ключ этого профиля создан не evo login: он остаётся в облаке — удалите его в консоли")
			}
			if err := config.DeleteProfile(name); err != nil {
				return err
			}
			out["deleted"] = name
			if printer.IsJSON() {
				return printer.JSON(out)
			}
			printer.Success(i18n.Tf("Profile %q deleted; its key: %s", "Профиль %q удалён; его ключ: %s", name, out["key"]))
			if n, ok := out["note"].(string); ok {
				printer.Warning(n)
			}
			return nil
		},
	}
}
