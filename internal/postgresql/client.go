// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package postgresql is a client for the Cloud.ru Managed PostgreSQL API
// (https://postgresql.api.cloud.ru, /v1). IAM Bearer auth. Cluster list/create
// is project-scoped via a `project_id` query/body field; per-cluster and
// reference endpoints (/v1/clusters/{id}, /v1/versions, /v1/operations/{id})
// are NOT project-scoped, so do() does not inject project_id — each method sets
// exactly what its endpoint needs. create/delete/backup are async → Operation.
package postgresql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

const defaultBaseURL = "https://postgresql.api.cloud.ru"

// Client talks to the Managed PostgreSQL v1 API.
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

// ProjectID exposes the configured project (for building request bodies).
func (c *Client) ProjectID() string { return c.projectID }

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
		return nil, 0, fmt.Errorf(i18n.T("postgresql request failed: %w", "запрос к postgresql не удался: %w"), err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	// grpc-gateway returns 4xx/5xx with an EMPTY body and the real reason in the
	// `grpc-message` response header (e.g. "backup disabled"). Surface it so the
	// error isn't a blank "postgresql API 400:".
	if len(data) == 0 {
		if gm := resp.Header.Get("grpc-message"); gm != "" {
			data = []byte(`{"message":` + strconv.Quote(gm) + `}`)
		}
	}
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
	return fmt.Errorf("postgresql API %d: %s", status, msg)
}

// Operation is the async result of create/delete/backup/switchover.
type Operation struct {
	ID         string          `json:"id"`
	ResourceID string          `json:"resource_id,omitempty"`
	Done       bool            `json:"done"`
	Error      *OperationError `json:"error,omitempty"`
}

// OperationError is google.rpc.Status.
type OperationError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// getList reads every page of a list: the answer carries next_page_token
// (openapi-postgresql.yaml), the next request sends it back as page_token. A list
// without that field is one page.
func (c *Client) getList(ctx context.Context, path string, query url.Values, field string) ([]map[string]any, error) {
	var items []map[string]any
	err := pager.All("postgresql: "+field, func(token string) (string, error) {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
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

func (c *Client) getObject(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, path, query, nil)
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

// --- Clusters ---

func (c *Client) projectQuery() url.Values {
	q := url.Values{}
	if c.projectID != "" {
		q.Set("project_id", c.projectID)
	}
	return q
}

// ListClusters lists clusters in the project.
func (c *Client) ListClusters(ctx context.Context) ([]map[string]any, error) {
	return c.getList(ctx, "/v1/clusters", c.projectQuery(), "clusters")
}

// GetCluster returns one cluster by id.
func (c *Client) GetCluster(ctx context.Context, id string) (map[string]any, error) {
	return c.getObject(ctx, "/v1/clusters/"+url.PathEscape(id), url.Values{})
}

// CreateCluster creates a cluster (async). Body per CreateClusterRequest.
func (c *Client) CreateCluster(ctx context.Context, body map[string]any) (*Operation, error) {
	return c.doOperation(ctx, http.MethodPost, "/v1/clusters", body)
}

// DeleteCluster deletes a cluster by id (async).
func (c *Client) DeleteCluster(ctx context.Context, id string) (*Operation, error) {
	return c.doOperation(ctx, http.MethodDelete, "/v1/clusters/"+url.PathEscape(id), nil)
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
		return nil, fmt.Errorf(i18n.T("parse operation: %w", "разбор операции: %w"), err)
	}
	return &op, nil
}

// --- Reference (needed to build a create request) ---

// ListVersions returns available PostgreSQL versions.
func (c *Client) ListVersions(ctx context.Context) ([]map[string]any, error) {
	return c.getList(ctx, "/v1/versions", url.Values{}, "versions")
}

// ListSpecifications returns specs (cpu/ram/mode/min_storage) for a version.
func (c *Client) ListSpecifications(ctx context.Context, versionName string) ([]map[string]any, error) {
	q := url.Values{}
	if versionName != "" {
		q.Set("version_name", versionName)
	}
	return c.getList(ctx, "/v1/specifications", q, "specifications")
}

// --- Databases ---

func (c *Client) ListDatabases(ctx context.Context, clusterID string) ([]map[string]any, error) {
	return c.getList(ctx, "/v1/clusters/"+url.PathEscape(clusterID)+"/databases", url.Values{}, "databases")
}

func (c *Client) CreateDatabase(ctx context.Context, clusterID string, body map[string]any) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodPost, "/v1/clusters/"+url.PathEscape(clusterID)+"/databases", url.Values{}, body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out, nil
}

func (c *Client) DeleteDatabase(ctx context.Context, clusterID, name string) error {
	data, status, err := c.do(ctx, http.MethodDelete, "/v1/clusters/"+url.PathEscape(clusterID)+"/databases/"+url.PathEscape(name), url.Values{}, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return apiError(status, data)
	}
	return nil
}

// --- Users ---

func (c *Client) ListUsers(ctx context.Context, clusterID string) ([]map[string]any, error) {
	return c.getList(ctx, "/v1/clusters/"+url.PathEscape(clusterID)+"/users", url.Values{}, "users")
}

func (c *Client) CreateUser(ctx context.Context, clusterID string, body map[string]any) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodPost, "/v1/clusters/"+url.PathEscape(clusterID)+"/users", url.Values{}, body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out, nil
}

func (c *Client) DeleteUser(ctx context.Context, clusterID, name string) error {
	data, status, err := c.do(ctx, http.MethodDelete, "/v1/clusters/"+url.PathEscape(clusterID)+"/users/"+url.PathEscape(name), url.Values{}, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return apiError(status, data)
	}
	return nil
}

// --- Backups ---

func (c *Client) ListBackups(ctx context.Context, clusterID string) ([]map[string]any, error) {
	return c.getList(ctx, "/v1/clusters/"+url.PathEscape(clusterID)+"/backups", url.Values{}, "backups")
}

func (c *Client) CreateBackup(ctx context.Context, clusterID, name string) (*Operation, error) {
	body := map[string]any{"cluster_id": clusterID, "name": name}
	return c.doOperation(ctx, http.MethodPost, "/v1/clusters/"+url.PathEscape(clusterID)+"/backups", body)
}
