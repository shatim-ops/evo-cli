// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package billing is a client for Cloud.ru billing/consumption (read-only).
// Two different hosts: account balance lives on billing.api.cloud.ru
// (GET /v1/agreement/{agreement_id}); usage/consumption lives on
// organization.api.cloud.ru (GET /v2/consumption?agreement_id=...&start_date=
// ...&end_date=...&page_filter.page_size=1000, paginated via next_page_token).
// IAM Bearer auth. Ported 1:1 from the legacy python cloudru_api.py so the Go
// engine fully replaces it.
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

// AgreementID exposes the configured billing agreement.
func (c *Client) AgreementID() string { return c.agreementID }

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

// ErrNoAgreement is the error of a billing call without an agreement id: a
// configuration problem, not an API error — CONFIG_INVALID (exit 2) with the
// variable to set; it was API_ERROR, exit 5 (release audit, L-06).
func ErrNoAgreement() error {
	return clierrors.New(clierrors.CodeConfigInvalid,
		"agreement_id not configured (set CLOUDRU_AGREEMENT_ID)",
		"Set CLOUDRU_AGREEMENT_ID to the billing agreement id in the environment of evo (or of the MCP server); see docs/auth.md.")
}

// Balance returns the raw agreement/balance object.
func (c *Client) Balance(ctx context.Context) (map[string]any, error) {
	if c.agreementID == "" {
		return nil, ErrNoAgreement()
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
// timestamps (e.g. 2026-07-01T00:00:00Z).
func (c *Client) Consumption(ctx context.Context, startISO, endISO string) ([]map[string]any, error) {
	if c.agreementID == "" {
		return nil, ErrNoAgreement()
	}
	var all []map[string]any
	pageToken := ""
	for {
		q := url.Values{}
		q.Set("agreement_id", c.agreementID)
		q.Set("start_date", startISO)
		q.Set("end_date", endISO)
		q.Set("page_filter.page_size", "1000")
		if pageToken != "" {
			q.Set("page_filter.page_token", pageToken)
		}
		u := c.consumptionBase + "/v2/consumption?" + q.Encode()
		data, status, err := c.get(ctx, u)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, apiError(status, data)
		}
		var out struct {
			Consumptions  []map[string]any `json:"consumptions"`
			NextPageToken string           `json:"next_page_token"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("parse consumption: %w", err)
		}
		all = append(all, out.Consumptions...)
		if out.NextPageToken == "" {
			break
		}
		pageToken = out.NextPageToken
	}
	return all, nil
}
