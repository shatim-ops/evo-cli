// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
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
			printer := output.New(flagJSON, flagNoColor)
			cfg, sources, err := resolveConfigWithSources(printer, scopeOverrides())
			if err != nil {
				return err
			}
			provider := tokenProvider(cfg)
			before := provider.Exchanges()
			tok, err := provider.Token(cmd.Context())
			if err != nil {
				return err
			}
			tokenSource := "cache"
			if provider.Exchanges() != before {
				tokenSource = "iam"
			}
			origin := sourceOrigin(sources, "KeyID")
			projectOrigin := sourceOrigin(sources, "ProjectID")
			visible, scopeErr := listProjects(cmd.Context(), tok.Value)

			if printer.IsJSON() {
				out := map[string]any{
					"profile":           cfg.Profile,
					"key_id":            config.MaskKeyID(cfg.KeyID),
					"credentials_from":  origin,
					"project_id":        cfg.ProjectID,
					"project_from":      projectOrigin,
					"token_expires_at":  tok.ExpiresAt.UTC().Format(time.RFC3339),
					"token_ttl_seconds": int(tok.TTL().Seconds()),
					"token_source":      tokenSource,
				}
				if scopeErr == nil {
					out["visible_projects"] = len(visible)
				}
				return printer.JSON(out)
			}
			project := cfg.ProjectID
			if project == "" {
				project = notSet()
			}
			w := printer.Writer()
			fmt.Fprintf(w, i18n.T("Profile:     %s\n", "Профиль:     %s\n"), cfg.Profile)
			fmt.Fprintf(w, i18n.T("Key ID:      %s (from %s)\n", "Key ID:      %s (источник: %s)\n"), config.MaskKeyID(cfg.KeyID), origin)
			if cfg.ProjectID != "" {
				project += i18n.T(" (from ", " (источник: ") + projectOrigin + ")"
			}
			fmt.Fprintf(w, i18n.T("Project ID:  %s\n", "Проект:      %s\n"), project)
			fmt.Fprintf(w, i18n.T("Projects:    %s\n", "Проекты:     %s\n"), scopeText(visible, scopeErr))
			fmt.Fprintf(w, i18n.T("Token:       valid until %s (%s left, %s)\n", "Токен:       действует до %s (осталось %s, %s)\n"),
				tok.ExpiresAt.Local().Format("2006-01-02 15:04:05 MST"),
				tok.TTL().Truncate(time.Minute), tokenSourceText(tokenSource))
			if cfg.ProjectID == "" {
				// The hint of the commands that refuse without a project.
				printer.Hint(clerrors.ProjectRequired().Hint)
			}
			if scopeErr == nil && len(visible) > 1 {
				printer.Warning(manyProjectsWarning(len(visible)))
			}
			return nil
		},
	}
}

func tokenSourceText(s string) string {
	if s == "cache" {
		return i18n.T("cached", "из кэша")
	}
	return i18n.T("issued by IAM now", "только что выдан IAM")
}

// notSet stands for an empty project ID.
func notSet() string { return i18n.T("(not set)", "(не задан)") }

// scopeText says how many projects the key sees. The Organization API does
// not list projects for every key (a service account gets 400 "user not
// found", 14.10.2026): then the scope is unknown, which is not an error.
func scopeText(visible []orgProject, err error) string {
	if err != nil {
		return i18n.T("unknown (the Organization API does not list projects for this key)",
			"неизвестно (Organization API не отдаёт список проектов для этого ключа)")
	}
	if len(visible) == 1 {
		return i18n.T("1 visible to the key", "ключ видит 1 проект")
	}
	n := len(visible)
	return i18n.Tf("%d visible to the key", "ключ видит %d "+i18n.Plural(n, "проект", "проекта", "проектов"), n)
}

// manyProjectsWarning explains what a key that sees several projects means:
// every command works in the project above, and an MCP server with this key
// can be told to work in any of them (QA P5).
func manyProjectsWarning(n int) string {
	return i18n.Tf("the key sees %d projects (organization-level access): commands work in the project above "+
		"unless --project-id says otherwise; limit a shared 'evo mcp --http' server with --allow-project",
		"ключ видит %d "+i18n.Plural(n, "проект", "проекта", "проектов")+" (доступ уровня организации): команды работают "+
			"в проекте выше, если --project-id не задаёт другой; общий сервер 'evo mcp --http' ограничьте флагом --allow-project", n)
}
