// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package nlb is a client for the Cloud.ru Evolution Load Balancer API
// (https://nlb.api.cloud.ru, /v2). IAM Bearer auth. Two resources:
// networkLoadBalancers (L4 balancer) and targetGroups (VM targets). List is
// project-scoped via a project_id query; create/delete are async → Operation.
package nlb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

const defaultBaseURL = "https://nlb.api.cloud.ru"

// Client talks to the Load Balancer v2 API.
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
	return &Client{http: httpretry.NewClient(60 * time.Second), base: baseURL, token: token, projectID: projectID}
}

// ProjectID exposes the configured project (for request bodies).
func (c *Client) ProjectID() string { return c.projectID }

// Operation is the async result of create/delete/update.
type Operation struct {
	ID         string          `json:"id"`
	ResourceID string          `json:"resource_id,omitempty"`
	Done       bool            `json:"done"`
	Error      *OperationError `json:"error,omitempty"`
	// CreatedAt and UpdatedAt are the operation's times, RFC 3339, as the
	// specification names them (tools: elapsed_sec of *_operation).
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// OperationError is rpcStatus.
type OperationError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, int, error) {
	u := c.base + path
	if enc := query.Encode(); enc != "" {
		u += "?" + enc
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf(i18n.T("nlb request failed: %w", "запрос к nlb не удался: %w"), err)
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
	return fmt.Errorf("nlb API %d: %s", status, msg)
}

// getList reads every page of a list (swagger.yaml of NLB: the answer
// carries next_page_token, the next request sends it as page_token).
func (c *Client) getList(ctx context.Context, path, field string) ([]map[string]any, error) {
	var items []map[string]any
	err := pager.All("nlb: "+field, func(token string) (string, error) {
		q := url.Values{}
		if c.projectID != "" {
			q.Set("project_id", c.projectID)
		}
		if token != "" {
			q.Set("page_token", token)
		}
		data, status, err := c.do(ctx, http.MethodGet, path, q, nil)
		if err != nil {
			return "", err
		}
		if status < 200 || status >= 300 {
			return "", apiError(status, data)
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(data, &out); err != nil {
			return "", fmt.Errorf(i18n.T("parse %s: %w", "разбор %s: %w"), field, err)
		}
		var page []map[string]any
		if raw, ok := out[field]; ok {
			_ = json.Unmarshal(raw, &page)
		}
		items = append(items, page...)
		var next string
		if raw, ok := out["next_page_token"]; ok {
			_ = json.Unmarshal(raw, &next)
		}
		return next, nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (c *Client) getObject(ctx context.Context, path string) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, path, url.Values{}, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf(i18n.T("parse response: %w", "разбор ответа: %w"), err)
	}
	return out, nil
}

func (c *Client) doOperation(ctx context.Context, method, path string, body any) (*Operation, error) {
	data, status, err := c.do(ctx, method, path, url.Values{}, body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf(i18n.T("parse operation: %w", "разбор операции: %w"), err)
	}
	return &op, nil
}

// --- Network Load Balancers ---

func (c *Client) ListNLB(ctx context.Context) ([]map[string]any, error) {
	return c.getList(ctx, "/v2/networkLoadBalancers", "balancers")
}

func (c *Client) GetNLB(ctx context.Context, id string) (map[string]any, error) {
	return c.getObject(ctx, "/v2/networkLoadBalancers/"+url.PathEscape(id))
}

func (c *Client) CreateNLB(ctx context.Context, body map[string]any) (*Operation, error) {
	return c.doOperation(ctx, http.MethodPost, "/v2/networkLoadBalancers", body)
}

func (c *Client) DeleteNLB(ctx context.Context, id string) (*Operation, error) {
	return c.doOperation(ctx, http.MethodDelete, "/v2/networkLoadBalancers/"+url.PathEscape(id), nil)
}

// --- Target Groups ---

func (c *Client) ListTargetGroups(ctx context.Context) ([]map[string]any, error) {
	return c.getList(ctx, "/v2/targetGroups", "target_groups")
}

func (c *Client) GetTargetGroup(ctx context.Context, id string) (map[string]any, error) {
	return c.getObject(ctx, "/v2/targetGroups/"+url.PathEscape(id))
}

func (c *Client) CreateTargetGroup(ctx context.Context, body map[string]any) (*Operation, error) {
	return c.doOperation(ctx, http.MethodPost, "/v2/targetGroups", body)
}

func (c *Client) DeleteTargetGroup(ctx context.Context, id string) (*Operation, error) {
	return c.doOperation(ctx, http.MethodDelete, "/v2/targetGroups/"+url.PathEscape(id), nil)
}

// --- Operations ---

func (c *Client) GetOperation(ctx context.Context, id string) (*Operation, error) {
	// The /v2/operations endpoint binds the id from the `id` QUERY param, not the
	// path — passing only the path yields 400 "id: required". Send both.
	q := url.Values{}
	q.Set("id", id)
	data, status, err := c.do(ctx, http.MethodGet, "/v2/operations/"+url.PathEscape(id), q, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf(i18n.T("parse operation: %w", "разбор операции: %w"), err)
	}
	return &op, nil
}
