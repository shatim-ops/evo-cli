// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// VMTask contains only the fields needed to scope and display the history.
type VMTask struct {
	ID         string     `json:"id"`
	EntityID   string     `json:"entity_id"`
	EntityType string     `json:"entity_type"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Created    time.Time  `json:"created_time"`
	Modified   *time.Time `json:"modified_time"`
}

type VMTaskPage struct {
	Items  []VMTask `json:"items"`
	Total  *int     `json:"total"`
	Offset *int     `json:"offset"`
	Limit  *int     `json:"limit"`
}

type VMTaskReader interface {
	VMTasks(ctx context.Context, project, vm string, offset, limit int) (*VMTaskPage, error)
}

func (a *CloudRuAdapter) VMTasks(ctx context.Context, project, vm string, offset, limit int) (*VMTaskPage, error) {
	q := url.Values{"project_id": {project}, "entity_id": {vm}, "entity_type": {"vm"}, "offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}}
	resp, err := a.doWithRetry(ctx, http.MethodGet, "/api/v1/tasks?"+q.Encode(), nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Compute error bodies can include identities from the task records.
		return nil, clierrors.New(clierrors.Reason(err), i18n.T("Could not read the Compute Tasks page.", "Не удалось прочитать страницу задач Compute."), "")
	}
	defer resp.Body.Close()
	var page VMTaskPage
	const maxBytes = 4 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil || len(data) > maxBytes || json.Unmarshal(data, &page) != nil {
		return nil, clierrors.New(clierrors.CodeAPIError, hintUnexpectedFormat(), "")
	}
	return &page, nil
}
