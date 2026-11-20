// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"net/http"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// GetInterface reads the Compute interface card, including a free interface.
func (a *CloudRuAdapter) GetInterface(ctx context.Context, id string) (*Interface, error) {
	if id == "" {
		return nil, clierrors.New(clierrors.CodeUsage, i18n.T("interface id is required", "нужен ID интерфейса"), "")
	}
	resp, err := a.doWithRetry(ctx, http.MethodGet, "/api/v1/interfaces/"+seg(id), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var card Interface
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to decode interface response", "не удалось разобрать ответ с данными интерфейса"), hintUnexpectedFormat(), err)
	}
	if card.ID != id {
		return nil, clierrors.New(clierrors.CodeAPIError, i18n.T("the interface card does not identify the requested interface", "в карточке нет ID запрошенного интерфейса"), hintUnexpectedFormat())
	}
	return &card, nil
}
