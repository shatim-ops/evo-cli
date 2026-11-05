// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

func newWhoamiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: i18n.T("Show the active profile and check its credentials", "Активный профиль и проверка его ключа"),
		Long: i18n.T(`Exchange the access key of the active profile for an IAM token and show
the profile, the key ID (masked), the project, the token expiry, the
key's roles on the project and its term (or why they are unknown).

A valid token cached by an earlier command is reused; IAM is not contacted
again until it is about to expire. Exits with 2 when the credentials are
missing or rejected.`, `Обменять ключ доступа активного профиля на токен IAM и показать профиль,
Key ID (маскированный), проект, срок действия токена, роли ключа на проекте
и его срок (или почему они неизвестны).

Действующий токен, сохранённый предыдущей командой, используется повторно:
к IAM команда обращается, только когда срок токена подходит к концу. Код
выхода 2 — ключа нет или IAM его отклонил.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := callEngineTool(cmd, "whoami", map[string]any{}, &tools.Caller{})
			if err != nil {
				return err
			}
			p := cmdPrinter(cmd)
			if p.IsJSON() {
				fmt.Fprintln(cmd.OutOrStdout(), text)
				return nil
			}
			return showWhoami(p, text)
		},
	}
	return engineCmd(cmd, "whoami")
}

// showWhoami is the answer of the whoami tool as 'evo whoami' shows it.
func showWhoami(p *output.Printer, text string) error {
	var a struct {
		Profile         string    `json:"profile"`
		KeyID           string    `json:"key_id"`
		CredentialsFrom string    `json:"credentials_from"`
		ProjectID       string    `json:"project_id"`
		ProjectFrom     string    `json:"project_from"`
		TokenExpiresAt  time.Time `json:"token_expires_at"`
		TokenTTLSeconds int       `json:"token_ttl_seconds"`
		TokenSource     string    `json:"token_source"`
		VisibleProjects *int      `json:"visible_projects"`
		Roles           *[]struct {
			Name      string `json:"name"`
			ExpiresAt string `json:"expires_at"`
		} `json:"roles"`
		RolesNote      string `json:"roles_note"`
		KeyExpiresAt   string `json:"key_expires_at"`
		KeyExpiresSoon bool   `json:"key_expires_soon"`
		KeyTermNote    string `json:"key_term_note"`
	}
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		return err
	}
	scope := tools.ScopeText(0, false)
	if a.VisibleProjects != nil {
		scope = tools.ScopeText(*a.VisibleProjects, true)
	}
	project := a.ProjectID
	if project == "" {
		project = notSet()
	} else {
		project += i18n.T(" (from ", " (источник: ") + a.ProjectFrom + ")"
	}
	w := p.Writer()
	fmt.Fprintf(w, i18n.T("Profile:     %s\n", "Профиль:     %s\n"), a.Profile)
	fmt.Fprintf(w, i18n.T("Key ID:      %s (from %s)\n", "Key ID:      %s (источник: %s)\n"), a.KeyID, a.CredentialsFrom)
	fmt.Fprintf(w, i18n.T("Project ID:  %s\n", "Проект:      %s\n"), project)
	fmt.Fprintf(w, i18n.T("Projects:    %s\n", "Проекты:     %s\n"), scope)
	fmt.Fprintf(w, i18n.T("Token:       valid until %s (%s left, %s)\n", "Токен:       действует до %s (осталось %s, %s)\n"),
		a.TokenExpiresAt.Local().Format("2006-01-02 15:04:05 MST"),
		(time.Duration(a.TokenTTLSeconds) * time.Second).Truncate(time.Minute), tokenSourceText(a.TokenSource))
	// The roles as IAM names them: the cloud's names, quoted (A0-2).
	roles := a.RolesNote
	if a.Roles != nil {
		var names []string
		for _, r := range *a.Roles {
			n := strconv.Quote(r.Name)
			if r.ExpiresAt != "" {
				n += i18n.Tf(" until %s", " до %s", r.ExpiresAt)
			}
			names = append(names, n)
		}
		roles = strings.Join(names, ", ")
		if len(names) == 0 {
			roles = i18n.T("none", "нет") + " — " + a.RolesNote
		}
	}
	if roles != "" {
		fmt.Fprintf(w, i18n.T("Roles:       %s\n", "Роли:        %s\n"), roles)
	}
	term := a.KeyTermNote
	if a.KeyExpiresAt != "" {
		term = i18n.Tf("valid until %s", "действует до %s", a.KeyExpiresAt)
	}
	if term != "" {
		fmt.Fprintf(w, i18n.T("Key term:    %s\n", "Срок ключа:  %s\n"), term)
	}
	if a.KeyExpiresSoon {
		p.Warning(i18n.Tf("the access key expires %s: a person issues a new one before that — 'evo login' or a new key in the console",
			"ключ доступа истекает %s: новый выпускает человек до этого срока — 'evo login' или новый ключ в консоли", a.KeyExpiresAt))
	}
	if a.ProjectID == "" {
		// The hint of the commands that refuse without a project.
		p.Hint(clerrors.ProjectRequired().Hint)
	}
	if a.VisibleProjects != nil && *a.VisibleProjects > 1 {
		p.Warning(tools.ManyProjectsWarning(*a.VisibleProjects))
	}
	return nil
}

func tokenSourceText(s string) string {
	if s == "cache" {
		return i18n.T("cached", "из кэша")
	}
	return i18n.T("issued by IAM now", "только что выдан IAM")
}

// notSet stands for an empty project ID.
func notSet() string { return i18n.T("(not set)", "(не задан)") }
