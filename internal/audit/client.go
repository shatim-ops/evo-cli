// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package audit is a client for the Cloud.ru Audit Logging API
// (https://audit.api.cloud.ru). IAM Bearer auth. Project is in the PATH
// (/events/list/{projectId}). Contract confirmed live — the API validated our
// request and returned "timestamps must end with 'Z' or a valid timezone
// offset" for start/end, i.e. RFC3339 (protobuf Timestamp), NOT Unix ms.
//
//	GET /events/list/{projectId}?start=&end=&limit=&nextPageToken=
//
// The page size is "limit" (spec, at most 500): an unknown parameter such
// as pageSize is answered 415 before the access check (register AUD-2).
//
// Recommended window: ≤30min for chatty projects, 1-3h for quiet ones.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

const defaultBaseURL = "https://audit.api.cloud.ru"

// Client talks to the Audit Logging API.
type Client struct {
	http      *http.Client
	base      string
	token     string
	projectID string
}

// New builds a client. baseURL empty → defaultBaseURL.
func New(token, projectID, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{http: httpretry.NewClient(60 * time.Second), base: strings.TrimRight(baseURL, "/"), token: token, projectID: projectID}
}

// ProjectID exposes the configured project.
func (c *Client) ProjectID() string { return c.projectID }

func (c *Client) do(ctx context.Context, path string, q url.Values) ([]byte, int, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("audit request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// MaxPageSize is the largest limit the API takes.
const MaxPageSize = 500

func apiError(status int, body []byte) error {
	// Access to audit logs is opened by Cloud.ru support («Доступ к
	// функциональности ограничен. Чтобы получать аудит-логи, обратитесь в
	// техническую поддержку» — docs, «Получить аудит-логи»). The API then
	// answers 401 to a valid token (register AUD-1): not a reason to log in
	// again. A 403 may as well be a role without access to the logs.
	if status == http.StatusForbidden {
		return clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("no access to audit logs: the key's role lacks it, or audit logging is not enabled for the project (audit API %d)",
				"нет доступа к аудит-логам: у роли ключа нет прав на них или аудит не включён для проекта (audit API %d)", status),
			i18n.T("Check the role of the service account in the Cloud.ru console; if it has access, audit logs are enabled by Cloud.ru support.",
				"Проверьте роль сервисного аккаунта в консоли Cloud.ru; если права есть — доступ к аудит-логам включает поддержка Cloud.ru."))
	}
	if status == http.StatusTooManyRequests {
		return clierrors.New(clierrors.CodeAPIRateLimited,
			i18n.Tf("audit API rate limit (audit API %d)", "превышена частота запросов к аудиту (audit API %d)", status),
			i18n.T("Retry in a minute.", "Повторите через минуту."))
	}
	if status == http.StatusUnauthorized {
		return clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("audit logging is not enabled for the project (audit API %d)", "аудит не включён для проекта (audit API %d)", status),
			i18n.T("Access to audit logs is enabled by Cloud.ru support: contact it (the key itself is valid if other commands work).",
				"Доступ к аудит-логам включает поддержка Cloud.ru — обратитесь в неё (сам ключ в порядке, если другие команды работают)."))
	}
	var e struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	msg := e.Message
	if msg == "" {
		msg = e.Error
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("audit API %d: %s", status, msg)
}

// EventPage is one page of audit events plus the continuation token.
type EventPage struct {
	Events        []map[string]any `json:"events"`
	NextPageToken string           `json:"nextPageToken"`
	// Logs is the array the spec (audit openapi-v3) names; Events is what
	// evo read so far. List merges them (api-contract §4.2).
	Logs []map[string]any `json:"logs,omitempty"`
}

// ListOptions filter an audit-events query. Start/End are RFC3339 (required by
// the API); if zero, the caller-provided defaults apply.
type ListOptions struct {
	Start    time.Time
	End      time.Time
	PageSize int
	// PageToken continues a previous page (from NextPageToken).
	PageToken string
}

// List returns one page of audit events for the project. The API paginates via
// nextPageToken; callers loop until it is empty. Response field names vary
// slightly by gateway version, so events are returned as raw maps.
func (c *Client) List(ctx context.Context, opts ListOptions) (*EventPage, error) {
	q := url.Values{}
	if !opts.Start.IsZero() {
		q.Set("start", opts.Start.UTC().Format(time.RFC3339))
	}
	if !opts.End.IsZero() {
		q.Set("end", opts.End.UTC().Format(time.RFC3339))
	}
	if opts.PageSize > 0 {
		q.Set("limit", fmt.Sprintf("%d", min(opts.PageSize, MaxPageSize)))
	}
	if opts.PageToken != "" {
		q.Set("nextPageToken", opts.PageToken)
	}
	data, status, err := c.do(ctx, "/events/list/"+url.PathEscape(c.projectID), q)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	page := &EventPage{}
	// Tolerate {events:[…],nextPageToken} or a bare array.
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		_ = json.Unmarshal(data, &page.Events)
		return page, nil
	}
	if err := json.Unmarshal(data, page); err != nil {
		return nil, fmt.Errorf("parse audit events: %w", err)
	}
	if len(page.Events) == 0 && len(page.Logs) > 0 {
		page.Events = page.Logs
	}
	page.Logs = nil
	return page, nil
}
