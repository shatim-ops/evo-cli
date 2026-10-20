// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// evo project list / use: an explicit "active project" kept in the profile,
// as 'az account list/set' and 'yc config profile' do, instead of a UUID
// typed by hand. The Organization API lists projects only to some keys (a
// project-level service account gets 400, 14.10.2026); then list says so and
// use takes the UUID as given.
func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: i18n.T("Projects the key sees and the project of the profile", "Проекты, которые видит ключ, и проект профиля"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newProjectListCmd(), newProjectUseCmd())
	return cmd
}

func newProjectUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <UUID|имя>",
		Short: i18n.T("Make a project the default of the profile", "Сделать проект проектом профиля по умолчанию"),
		Long: i18n.T(`Write the project into the active profile: commands then work in it
unless --project-id, CLOUDRU_PROJECT_ID or ./.evo.yaml say otherwise.

The project is given by UUID or, when the key can list its projects, by name.
A project the key does not see is refused. When the Organization API does not
list projects for the key, the UUID is saved as given.`,
			`Записать проект в активный профиль: команды работают в нём, если
--project-id, CLOUDRU_PROJECT_ID или ./.evo.yaml не указывают другой.

Проект задаётся UUID или, если ключ может получить список проектов, именем.
Проект, которого ключ не видит, не принимается. Если Organization API не
отдаёт этому ключу список проектов, UUID сохраняется как есть.`),
		Example: `  evo project use 3f2a8c1e-0000-0000-0000-000000000000
  evo project use my-team-dev
  evo --profile test project use <UUID>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			want := strings.TrimSpace(args[0])
			name, _ := config.ProfileName(flagProfile)
			p, c, ok, err := config.LoadProfile(name)
			if err != nil {
				return err
			}
			if !ok {
				return clerrors.New(clerrors.CodeConfigInvalid,
					i18n.Tf("profile %q not found", "профиль %q не найден", name),
					i18n.T("Create it with 'evo configure'.", "Создайте его командой 'evo configure'."))
			}
			cfg, err := resolveConfig(printer, scopeOverrides())
			if err != nil {
				return err
			}
			id, label, err := resolveProjectArg(cmd, cfg, want)
			if err != nil {
				return err
			}
			p.ProjectID = id
			if err := config.SaveProfile(name, p, c); err != nil {
				return err
			}
			if printer.IsJSON() {
				return printer.JSON(map[string]any{"profile": name, "project_id": id, "name": label})
			}
			shown := id
			if label != "" {
				shown += " (" + label + ")"
			}
			printer.Success(i18n.Tf("Project of profile %s: %s", "Проект профиля %s: %s", name, shown))
			if env := os.Getenv("CLOUDRU_PROJECT_ID"); env != "" && env != id {
				printer.Warning(i18n.Tf("CLOUDRU_PROJECT_ID=%s is set and takes precedence over the profile",
					"задана CLOUDRU_PROJECT_ID=%s — она важнее профиля", env))
			}
			return nil
		},
	}
}

// resolveProjectArg turns a UUID or a name into a project ID the key sees.
// Without a project list (the key may not have one) a UUID is taken as is
// and a name cannot be resolved.
func resolveProjectArg(cmd *cobra.Command, cfg config.Config, want string) (id, name string, err error) {
	tok, err := tokenProvider(cfg).Token(cmd.Context())
	if err != nil {
		return "", "", err
	}
	projects, listErr := listProjects(cmd.Context(), tok.Value)
	if listErr != nil {
		if !looksLikeUUID(want) {
			return "", "", clerrors.Wrap(clerrors.CodeConfigInvalid,
				i18n.Tf("cannot find project %q by name: the key cannot list projects", "не найти проект %q по имени: ключ не может получить список проектов", want),
				i18n.T("Pass the project UUID (from the Cloud.ru console).", "Укажите UUID проекта (он есть в консоли Cloud.ru)."), listErr)
		}
		output.New(flagJSON, flagNoColor).Warning(i18n.T(
			"the project list is not available to this key: the UUID is saved unchecked",
			"список проектов этому ключу недоступен: UUID сохранён без проверки"))
		return want, "", nil
	}
	var byName []orgProject
	for _, p := range projects {
		if p.ID == want {
			return p.ID, p.Name, nil
		}
		if p.Name == want {
			byName = append(byName, p)
		}
	}
	switch len(byName) {
	case 1:
		return byName[0].ID, byName[0].Name, nil
	case 0:
		return "", "", clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("the key does not see project %q", "ключ не видит проект %q", want),
			i18n.T("See the projects with 'evo project list'.", "Список проектов: 'evo project list'."))
	}
	ids := make([]string, len(byName))
	for i, p := range byName {
		ids[i] = p.ID
	}
	return "", "", clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("several projects are named %q: %s", "несколько проектов с именем %q: %s", want, strings.Join(ids, ", ")),
		i18n.T("Pass the UUID.", "Укажите UUID."))
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}

// scopeOverrides passes --project-id and --region to the config.
func scopeOverrides() map[string]any {
	o := map[string]any{}
	applyScopeFlags(o)
	return o
}
