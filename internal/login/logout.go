// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"net/http"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/iamadmin"
)

// The statuses of the key in the answer of logout.
const (
	KeyDeleted        = "deleted"
	KeyAlreadyDeleted = "already_deleted" // D26: the key was gone already
)

// Logout deletes the key evo login made — by its id, with the token of its
// own account (R-25) — and only it: the account and its role stay. The
// caller deletes the profile after this answers; an error leaves the
// profile, so nothing is lost that still works.
func Logout(ctx context.Context, adminBase string, key auth.TokenProvider, serviceAccountID, accessKeyID string) (string, error) {
	err := iamadmin.New(key, adminBase).DeleteAccessKey(ctx, serviceAccountID, accessKeyID)
	switch {
	case err == nil:
		return KeyDeleted, nil
	case iamadmin.StatusOf(err) == http.StatusNotFound:
		return KeyAlreadyDeleted, nil
	}
	return "", clierrors.Wrap(clierrors.CodeAPIError,
		i18n.Tf("the access key %s was not deleted: %v; the profile is kept", "ключ доступа %s не удалён: %v; профиль оставлен", accessKeyID, err),
		i18n.T("Delete the key in the console (Users → Service accounts → the account → Access keys), then 'evo configure delete <profile>'.",
			"Удалите ключ в консоли («Пользователи» → «Сервисные аккаунты» → аккаунт → «Ключи доступа»), затем 'evo configure delete <профиль>'."), err)
}
