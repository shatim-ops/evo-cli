// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package floatingip

import (
	"context"
	"errors"
	"net/http"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// NotFound also accepts VM_NOT_FOUND from older Compute adapters.
func NotFound(err error) bool {
	var cliErr *clerrors.CLIError
	return provision.HTTPStatus(err) == http.StatusNotFound ||
		(errors.As(err, &cliErr) && (cliErr.Code == clerrors.CodeNotFound || cliErr.Code == clerrors.CodeVMNotFound))
}

// Gone confirms that the address in a receipt is gone by reading its own
// card. A successful DELETE, an incomplete response or a failed read do
// not settle a billed resource.
func Gone(ctx context.Context, cloud provision.CloudAPI, id string) (bool, error) {
	fip, err := cloud.GetFloatingIP(ctx, id)
	if NotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if fip == nil || fip.ID != id {
		return false, errors.New(i18n.T("floating IP read did not return the requested ID", "чтение floating IP не вернуло запрошенный ID"))
	}
	return false, nil
}
