// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
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
the profile, the key ID (masked), the project and the token expiry.

A valid token cached by an earlier command is reused; IAM is not contacted
again until it is about to expire. Exits with 2 when the credentials are
missing or rejected.`, `Обменять ключ доступа активного профиля на токен IAM и показать профиль,
Key ID (маскированный), проект и срок действия токена.

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
