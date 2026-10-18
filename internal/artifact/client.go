// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package artifact is a client for the Cloud.ru Artifact Registry API
// (https://ar.api.cloud.ru, /v1). IAM Bearer auth. Registries are
// project-scoped via a projectId query (list/get/delete) or body field
// (create/patch). Create & delete are ASYNC → Operation, poll /v1/operations/{id}.
// A registry holds Docker images / Helm charts (also Debian/RPM/PyPI/Generic) —
// the source for Container Apps / mk8s images that docker.io can't provide.
package artifact

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
	"github.com/shatim-ops/evo-cli/internal/pager"
)

const defaultBaseURL = "https://ar.api.cloud.ru"

// Client talks to the Artifact Registry v1 API.
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

// Operation is the async result of registry create/delete.
type Operation struct {
	ID         string          `json:"id"`
	ResourceID string          `json:"resourceId,omitempty"`
	Done       bool            `json:"done"`
	Error      *OperationError `json:"error,omitempty"`
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
		return nil, 0, fmt.Errorf("artifact-registry request failed: %w", err)
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
	return fmt.Errorf("artifact-registry API %d: %s", status, msg)
}

func (c *Client) projectQuery() url.Values {
	q := url.Values{}
	if c.projectID != "" {
		q.Set("projectId", c.projectID)
	}
	return q
}

func (c *Client) doOperation(ctx context.Context, method, path string, query url.Values, body any) (*Operation, error) {
	data, status, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf("parse operation: %w", err)
	}
	return &op, nil
}

// ListRegistries lists registries in the project, every page
// (ar/openapi.yaml: nextPageToken in the answer, pageToken in the next
// request). ar_create checks a name against it, so a page left unread
// let a taken name through (#98).
func (c *Client) ListRegistries(ctx context.Context) ([]map[string]any, error) {
	var all []map[string]any
	err := pager.All("artifact-registry: registries", func(token string) (string, error) {
		q := c.projectQuery()
		if token != "" {
			q.Set("pageToken", token)
		}
		data, status, err := c.do(ctx, http.MethodGet, "/v1/registries", q, nil)
		if err != nil {
			return "", err
		}
		if status < 200 || status >= 300 {
			return "", apiError(status, data)
		}
		var out struct {
			Registries    []map[string]any `json:"registries"`
			NextPageToken string           `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return "", fmt.Errorf("parse registries: %w", err)
		}
		all = append(all, out.Registries...)
		return out.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// GetRegistry returns one registry by id.
func (c *Client) GetRegistry(ctx context.Context, id string) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, "/v1/registries/"+url.PathEscape(id), c.projectQuery(), nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return out, nil
}

// CreateRegistry creates a registry (async). body must include projectId + name.
func (c *Client) CreateRegistry(ctx context.Context, body map[string]any) (*Operation, error) {
	return c.doOperation(ctx, http.MethodPost, "/v1/registries", url.Values{}, body)
}

// DeleteRegistry deletes a registry by id (async).
func (c *Client) DeleteRegistry(ctx context.Context, id string) (*Operation, error) {
	return c.doOperation(ctx, http.MethodDelete, "/v1/registries/"+url.PathEscape(id), c.projectQuery(), nil)
}

// GetOperation polls an async operation by id.
func (c *Client) GetOperation(ctx context.Context, id string) (*Operation, error) {
	data, status, err := c.do(ctx, http.MethodGet, "/v1/operations/"+url.PathEscape(id), url.Values{}, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf("parse operation: %w", err)
	}
	return &op, nil
}
