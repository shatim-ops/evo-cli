// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// newConfigureS3Cmd sets the S3 keys of a profile from its own access key
// (agent run M2, Ц-10): Object Storage takes the same key in another form —
// access_key <tenant_id>:<Key ID>, secret_key the Key Secret (docs/auth.md,
// «Ключи Object Storage (S3)»). Only the tenant ID is new, so no secret is
// typed again and none goes through argv.
func newConfigureS3Cmd() *cobra.Command {
	var tenant string
	cmd := &cobra.Command{
		Use:   "s3",
		Short: i18n.T("Set the S3 keys of the profile from its access key and the tenant ID", "Ключи S3 профиля из его ключа доступа и tenant ID"),
		Long: i18n.T(`Set the S3 keys of the profile (the s3_* tools and evo s3) from its access key:
Object Storage takes the same key as access_key <tenant_id>:<Key ID> and
secret_key the Key Secret. The tenant ID is in the console: Storage, Object
Storage, API settings. The account needs a role of Object Storage
(s3e.viewer to read, s3e.editor to write). Check: evo s3 buckets.`,
			`Ключи S3 профиля (инструменты s3_* и evo s3) из его ключа доступа:
Object Storage принимает тот же ключ — access_key <tenant_id>:<Key ID>,
secret_key — Key Secret. tenant ID — в консоли: «Хранение данных → Object
Storage → Параметры работы с API». Аккаунту нужна роль Object Storage
(s3e.viewer для чтения, s3e.editor для записи). Проверка: evo s3 buckets.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigureS3(tenant, output.New(flagJSON, flagNoColor))
		},
	}
	cmd.Flags().StringVar(&tenant, "tenant-id", "", i18n.T("Tenant ID of Object Storage (console: Object Storage, API settings); required",
		"tenant ID Object Storage (консоль: Object Storage → Параметры работы с API); обязательный"))
	return cmd
}

// runConfigureS3 writes the s3 section of the profile: the access key with
// the tenant ID, the Key Secret, the endpoint and region kept as they were.
func runConfigureS3(tenant string, printer *output.Printer) error {
	if tenant == "" || strings.ContainsAny(tenant, " \t\r\n:.") {
		return clerrors.New(clerrors.CodeUsage,
			i18n.Tf("tenant ID %q: one word, without spaces, ':' or '.'", "tenant ID %q: одно слово, без пробелов, ':' и '.'", tenant),
			i18n.T("Pass --tenant-id from the console: Storage, Object Storage, API settings.",
				"Передайте --tenant-id из консоли: «Хранение данных → Object Storage → Параметры работы с API»."))
	}
	name, _ := config.ProfileName(flagProfile)
	if config.Dir() == "" {
		return config.NoConfigDir()
	}
	prof, creds, _, err := config.LoadProfile(name)
	if err != nil {
		return err
	}
	if creds.KeyID == "" || creds.Secret == "" {
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("profile %q has no access key to build the S3 keys from", "в профиле %q нет ключа доступа, из которого строятся ключи S3", name),
			i18n.T("Run 'evo configure' first, then 'evo configure s3 --tenant-id <tenant_id>'.",
				"Сначала 'evo configure', затем 'evo configure s3 --tenant-id <tenant_id>'."))
	}
	s3 := config.S3Config{}
	if creds.S3 != nil {
		s3 = *creds.S3
	}
	s3.AccessKey, s3.SecretKey = tenant+":"+creds.KeyID, creds.Secret
	creds.S3 = &s3
	if dryRunWrite(context.Background(), confirm.Preview{Action: "configure s3",
		Summary: i18n.Tf("save the S3 keys of the tenant %s to the profile %q", "сохранить ключи S3 тенанта %s в профиль %q", tenant, name),
		Details: map[string]any{"profile": name, "s3_access_key": tenant + ":" + config.MaskKeyID(creds.KeyID)}}) {
		return nil
	}
	if err := config.SaveProfile(name, prof, creds); err != nil {
		return err
	}
	shown := tenant + ":" + config.MaskKeyID(creds.KeyID)
	if printer.IsJSON() {
		return printer.JSON(map[string]any{"profile": name, "s3_access_key": shown, "config_dir": config.Dir()})
	}
	printer.Success(i18n.Tf("S3 keys of profile %q saved: access_key %s, secret_key the Key Secret of the profile",
		"Ключи S3 профиля %q сохранены: access_key %s, secret_key — Key Secret профиля", name, shown))
	printer.Hint(i18n.T("Check: evo s3 buckets", "Проверка: evo s3 buckets"))
	return nil
}
