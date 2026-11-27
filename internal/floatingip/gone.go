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

// NotFound accepts an unclassified DELETE 404 to proceed to a verifying GET.
func NotFound(err error) bool {
	var cliErr *clerrors.CLIError
	return provision.HTTPStatus(err) == http.StatusNotFound ||
		(errors.As(err, &cliErr) && (cliErr.Code == clerrors.CodeNotFound || cliErr.Code == clerrors.CodeVMNotFound))
}

// ServiceNotFound requires the adapter's classified resource-not-found.
// A generic API error carrying 404 may refer to a missing gateway route.
func ServiceNotFound(err error) bool {
	reason := clerrors.Reason(err)
	status := provision.HTTPStatus(err)
	return (reason == clerrors.CodeNotFound || reason == clerrors.CodeVMNotFound) && (status == 0 || status == http.StatusNotFound)
}

// Gone confirms that the address in a receipt is gone by reading its own
// card. A successful DELETE, an incomplete response or a failed read do
// not settle a billed resource.
func Gone(ctx context.Context, cloud provision.CloudAPI, id string) (bool, error) {
	fip, err := provision.ReadFloatingIPForCleanup(ctx, cloud, id)
	if ServiceNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if fip == nil || fip.ID != id {
		return false, clerrors.New(clerrors.CodeAPIError, i18n.T("The service did not return the requested floating IP ID. Its release is unconfirmed.", "Сервис не вернул запрошенный ID плавающего IP. Освобождение не подтверждено."), "")
	}
	return false, nil
}
