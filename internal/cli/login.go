// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
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
	cmd := &cobra.Command{
		Use:   "login",
		Short: i18n.T("Sign in to Cloud.ru in a browser and keep an access key in the profile", "Войти в Cloud.ru через браузер и записать ключ доступа в профиль"),
		Long: i18n.T(`Sign in to Cloud.ru in a browser; on the page evo serves on this machine,
choose the project, whom the key is for (you: account evo; an agent:
account evo-agent) and its term (7, 30 or 90 days). With your rights evo
makes or reuses the service account, grants it the role «project
administrator» until the key's term, creates the key and keeps it in the
profile. The role includes deleting resources. A previous key evo login
made for this profile is revoked. Your tokens stay in memory; the key's
secret is shown nowhere. Undo: evo logout.`, `Войти в Cloud.ru через браузер; на странице, которую evo открывает на этой
машине, выбрать проект, для кого ключ (вы — аккаунт evo, агент — аккаунт
evo-agent) и его срок (7, 30 или 90 дней). Вашими правами evo создаёт или
переиспользует сервисный аккаунт, выдаёт ему роль «Администратор проекта»
до срока ключа, создаёт ключ и записывает его в профиль. Роль включает
удаление ресурсов. Прежний ключ evo login этого профиля отзывается. Ваши
токены остаются в памяти; секрет ключа нигде не показывается. Отменить —
evo logout.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			choice, err := loginChoice(project, forWhom, ttl)
			if err != nil {
				return err
			}
			name, _ := config.ProfileName(flagProfile)
			if err := config.ValidateProfileName(name); err != nil {
				return err
			}
			prof, creds, _, err := config.LoadProfile(name)
			if err != nil {
				return err
			}
			var prev *login.Previous
			if creds.Login != nil {
				prev = &login.Previous{ServiceAccountID: creds.Login.ServiceAccountID, AccessKeyID: creds.Login.AccessKeyID}
			}
			printer := output.New(flagJSON, flagNoColor)
			res, err := login.Login(cmd.Context(), loginConfig(loginSay(cmd), openBrowser), choice, prev, func(s login.Saved) error {
				prof.ProjectID = s.ProjectID
				creds.KeyID, creds.Secret = s.KeyID, s.Secret
				creds.Login = &config.LoginKey{ServiceAccountID: s.ServiceAccountID, AccessKeyID: s.AccessKeyID,
					KeyExpiresAt: s.KeyExpiresAt.UTC().Format(time.RFC3339), CustomerID: s.CustomerID, Role: s.Role,
					LoggedInAs: s.Login, LoginAt: s.LoginAt.UTC().Format(time.RFC3339)}
				return config.SaveProfile(name, prof, creds)
			})
			if err != nil {
				return err
			}
			out := map[string]any{
				"profile": name, "key_id": config.MaskKeyID(res.KeyID), "project_id": res.ProjectID,
				"service_account": res.Account, "account_reused": res.AccountReused, "role": res.Role,
				"key_expires_at": res.KeyExpiresAt.UTC().Format(time.RFC3339), "previous_key_revoked": res.PreviousRevoked,
			}
			if res.PreviousNote != "" {
				out["warning"] = res.PreviousNote
			}
			if printer.IsJSON() {
				return printer.JSON(out)
			}
			printer.Success(i18n.Tf("Signed in: the key of %s for the project %s is kept in the profile %q until %s",
				"Вход выполнен: ключ %s для проекта %s записан в профиль %q до %s",
				res.Account, res.ProjectName, name, res.KeyExpiresAt.Local().Format("2006-01-02")))
			if res.PreviousNote != "" {
				printer.Warning(res.PreviousNote)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", i18n.T("Project id or name to offer alone on the page", "id или имя проекта — на странице будет только он"))
	cmd.Flags().StringVar(&forWhom, "for", "", i18n.T("Whom the key is for: me (account evo) or agent (account evo-agent); chosen on the page by default",
		"Для кого ключ: me (аккаунт evo) или agent (аккаунт evo-agent); по умолчанию выбирается на странице"))
	cmd.Flags().StringVar(&ttl, "key-ttl", "", i18n.T("Term of the key: 7d, 30d or 90d (30d by default)", "Срок ключа: 7d, 30d или 90d (по умолчанию 30d)"))
	return cmd
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
