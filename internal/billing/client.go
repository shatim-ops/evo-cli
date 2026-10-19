// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package billing is a client for Cloud.ru billing/consumption (read-only).
// Two different hosts: account balance lives on billing.api.cloud.ru
// (GET /v1/agreement/{agreement_id}); usage/consumption lives on
// organization.api.cloud.ru (GET /v2/consumption?agreement_id=...&start_date=
// ...&end_date=...&page_filter.page_size=1000, paginated via next_page_token).
// Consumption is filtered by the agreement or by a project (project_ids):
// both filters are optional in the spec «Контроль затрат» v2, and a project
// administrator (platform.project.admin) may read the consumption of the
// project («Перед началом работы», api-ref_start) without the agreement id,
// which only organization and cost administrators see. IAM Bearer auth.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

// Documentation pages the hints point to.
const (
	// DocAPIStart lists the roles that may read consumption: organization
	// and cost administrators for every agreement and project, the project
	// administrator for the project.
	DocAPIStart = "https://cloud.ru/docs/billing/ug/topics/api-ref_start"
	// DocAgreementID: where the agreement id is and who can see it.
	DocAgreementID = "https://cloud.ru/docs/billing/ug/topics/faq__api_id_agreement"
)

const (
	defaultBalanceBase     = "https://billing.api.cloud.ru"
	defaultConsumptionBase = "https://organization.api.cloud.ru"
)

// Client talks to the billing + consumption read APIs.
type Client struct {
	http            *http.Client
	balanceBase     string
	consumptionBase string
	token           string
	agreementID     string
	projectID       string
}

// New builds a client. Empty bases fall back to the Cloud.ru defaults.
func New(token, agreementID, balanceBase, consumptionBase string) *Client {
	if balanceBase == "" {
		balanceBase = defaultBalanceBase
	}
	if consumptionBase == "" {
		consumptionBase = defaultConsumptionBase
	}
	return &Client{
		http:            httpretry.NewClient(60 * time.Second),
		balanceBase:     balanceBase,
		consumptionBase: consumptionBase,
		token:           token,
		agreementID:     agreementID,
	}
}

// WithProject filters consumption by the project (project_ids). With an
// agreement too, both filters apply: the project within the agreement.
func (c *Client) WithProject(projectID string) *Client {
	c.projectID = projectID
	return c
}

// AgreementID exposes the configured billing agreement.
func (c *Client) AgreementID() string { return c.agreementID }

// ProjectID is the project consumption is filtered by ("" — none).
func (c *Client) ProjectID() string { return c.projectID }

func (c *Client) get(ctx context.Context, u string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("billing request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func apiError(status int, body []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	msg := e.Message
	if msg == "" {
		msg = string(body)
	}
	return fmt.Errorf("billing API %d: %s", status, msg)
}

// ErrNoAgreement is the error of a consumption call with neither an
// agreement nor a project: a configuration problem, not an API error —
// CONFIG_INVALID (exit 2) with what to set; it was API_ERROR, exit 5
// (release audit, L-06).
func ErrNoAgreement() error {
	return clierrors.New(clierrors.CodeConfigInvalid,
		i18n.T("neither a project nor a billing agreement is set (project_id, CLOUDRU_AGREEMENT_ID)",
			"не задан ни проект, ни договор (project_id, CLOUDRU_AGREEMENT_ID)"),
		i18n.T("Consumption of a project needs the project: 'evo configure --project-id <UUID>', CLOUDRU_PROJECT_ID or the argument project_id (the key's role: project administrator, platform.project.admin). For the whole agreement set CLOUDRU_AGREEMENT_ID (seen only by organization and cost administrators: "+DocAgreementID+"); see docs/auth.md.",
			"Для расхода проекта задайте проект: 'evo configure --project-id <UUID>', CLOUDRU_PROJECT_ID или аргумент project_id (роль ключа — администратор проекта, platform.project.admin). Для всего договора задайте CLOUDRU_AGREEMENT_ID (его видят только администратор организации и администратор затрат: "+DocAgreementID+"); см. docs/auth.md."))
}

// ErrBalanceNoAgreement is the error of a balance call without an agreement
// id: the balance belongs to the agreement, and no published API gives it
// by project — CONFIG_INVALID (exit 2), as ErrNoAgreement.
func ErrBalanceNoAgreement() error {
	return clierrors.New(clierrors.CodeConfigInvalid,
		i18n.T("the balance needs the billing agreement (CLOUDRU_AGREEMENT_ID): the API has no balance by project",
			"для баланса нужен договор (CLOUDRU_AGREEMENT_ID): баланса по проекту в API нет"),
		i18n.T("Set CLOUDRU_AGREEMENT_ID in the environment of evo (or of the MCP server); the agreement id is seen only by organization and cost administrators ("+DocAgreementID+"). The spending of the project without it: billing_consumption (role platform.project.admin).",
			"Задайте CLOUDRU_AGREEMENT_ID в окружении evo (или MCP-сервера); ID договора видят только администратор организации и администратор затрат ("+DocAgreementID+"). Расход проекта без договора — billing_consumption (роль platform.project.admin)."))
}

// consumptionError is a non-2xx answer of /v2/consumption. A 403 is the
// role of the key (the spec: «Доступ запрещен»): API_ERROR, exit 5, with the
// role that may read this filter, as the audit client does for its 403.
func (c *Client) consumptionError(status int, body []byte) error {
	if status != http.StatusForbidden {
		return apiError(status, body)
	}
	if c.agreementID == "" {
		return clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("no access to the consumption of project %s: the key's role lacks it (billing API %d)",
				"нет доступа к потреблению проекта %s: у роли ключа нет прав (billing API %d)", c.projectID, status),
			i18n.T("Consumption of a project is read by its project administrator (platform.project.admin): give the service account this role in the project, "+DocAPIStart+".",
				"Потребление проекта доступно администратору проекта (platform.project.admin): дайте сервисному аккаунту эту роль в проекте, "+DocAPIStart+"."))
	}
	return clierrors.New(clierrors.CodeAPIError,
		i18n.Tf("no access to the consumption of agreement %s: the key's role lacks it (billing API %d)",
			"нет доступа к потреблению договора %s: у роли ключа нет прав (billing API %d)", c.agreementID, status),
		i18n.T("Consumption of an agreement is read by the organization administrator (platform.customer.admin) or the cost administrator (platform.customer.expense-admin); a project administrator reads its project: unset CLOUDRU_AGREEMENT_ID, "+DocAPIStart+".",
			"Потребление договора доступно администратору организации (platform.customer.admin) и администратору затрат (platform.customer.expense-admin); администратору проекта — потребление проекта: уберите CLOUDRU_AGREEMENT_ID, "+DocAPIStart+"."))
}

// Balance returns the raw agreement/balance object.
func (c *Client) Balance(ctx context.Context) (map[string]any, error) {
	if c.agreementID == "" {
		return nil, ErrBalanceNoAgreement()
	}
	u := c.balanceBase + "/v1/agreement/" + url.PathEscape(c.agreementID)
	data, status, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse balance: %w", err)
	}
	return out, nil
}

// Consumption returns all consumption line items in [startISO, endISO],
// following next_page_token pagination. startISO/endISO are full RFC3339
// timestamps (e.g. 2026-07-01T00:00:00Z). The filter is the agreement, the
// project (WithProject) or both; with neither it is ErrNoAgreement, before
// any request.
func (c *Client) Consumption(ctx context.Context, startISO, endISO string) ([]map[string]any, error) {
	if c.agreementID == "" && c.projectID == "" {
		return nil, ErrNoAgreement()
	}
	var all []map[string]any
	// Every page through internal/pager, with a bound: a server that sent
	// the same next_page_token again looped forever and grew the list until
	// the process ran out of memory (QA 20.10, QA-2). A repeated token is an
	// error here, not the end of the list: the sum of a list read twice or
	// cut short is a wrong amount of money.
	sent := map[string]bool{}
	more, err := pager.Upto(pager.MaxPages, func(pageToken string) (string, error) {
		sent[pageToken] = true
		q := url.Values{}
		if c.agreementID != "" {
			q.Set("agreement_id", c.agreementID)
		}
		if c.projectID != "" {
			q.Set("project_ids", c.projectID)
		}
		q.Set("start_date", startISO)
		q.Set("end_date", endISO)
		q.Set("page_filter.page_size", "1000")
		if pageToken != "" {
			q.Set("page_filter.page_token", pageToken)
		}
		u := c.consumptionBase + "/v2/consumption?" + q.Encode()
		data, status, err := c.get(ctx, u)
		if err != nil {
			return "", err
		}
		if status < 200 || status >= 300 {
			return "", c.consumptionError(status, data)
		}
		var out struct {
			Consumptions  []map[string]any `json:"consumptions"`
			NextPageToken string           `json:"next_page_token"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return "", fmt.Errorf("parse consumption: %w", err)
		}
		all = append(all, out.Consumptions...)
		if out.NextPageToken != "" && sent[out.NextPageToken] {
			return "", clierrors.New(clierrors.CodeAPIError,
				i18n.Tf("billing API: consumption: the answer repeats page token %q: the list never ends, the sums would be wrong",
					"billing API: потребление: ответ повторяет токен страницы %q: список не кончается, суммы были бы неверны", out.NextPageToken),
				i18n.T("Cloud.ru answered a broken page sequence; retry later, or read a shorter period.",
					"Cloud.ru вернул сломанную последовательность страниц; повторите позже или запросите период короче."))
		}
		return out.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	if more {
		return nil, clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("billing API: consumption: more than %d pages, the list is not complete", "billing API: потребление: больше %d страниц, список неполный", pager.MaxPages),
			i18n.T("Read a shorter period (start, end).", "Запросите период короче (start, end)."))
	}
	return all, nil
}
