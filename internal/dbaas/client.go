// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package dbaas is a generic client for Cloud.ru Managed-Database services that
// share the Evolution "dbaas/v1" shape — Kafka (kafka.api.cloud.ru) and Redis
// (redis.api.cloud.ru), which mirror the PostgreSQL API. IAM Bearer auth,
// snake_case, project_id query. create/delete are async → Operation; poll
// /v1/operations/{id}. Each service instantiates this with its own host; the
// create-request body differs per service and is passed through verbatim.
package dbaas

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

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

// Client talks to a Managed-DB v1 API (Kafka/Redis).
type Client struct {
	http      *http.Client
	base      string
	token     string
	projectID string
}

// New builds a client for baseURL (e.g. https://kafka.api.cloud.ru).
func New(token, projectID, baseURL string) *Client {
	return &Client{http: httpretry.NewClient(60 * time.Second), base: baseURL, token: token, projectID: projectID}
}

// ProjectID exposes the configured project.
func (c *Client) ProjectID() string { return c.projectID }

// Operation is the async result of create/delete.
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
		return nil, 0, fmt.Errorf(i18n.T("dbaas request failed: %w", "запрос к dbaas не удался: %w"), err)
	}
	return ReadAPIResponse(req, resp)
}

// ReadAPIResponse closes the response and returns a complete representation, including
// the gRPC message carried by the gateway header on an otherwise empty body.
func ReadAPIResponse(req *http.Request, resp *http.Response) ([]byte, int, error) {
	defer resp.Body.Close()
	data, status, err := httpretry.ReadAPIResponse(req, resp, 0)
	if err != nil {
		// Keep cancellation and timeout intact. Only an incomplete body
		// at a successful status gets the cluster list's diagnostic.
		if clierrors.Reason(err) == clierrors.CodeAPIError && req.Method == http.MethodGet && req.URL.Path == "/v1/clusters" && status >= 200 && status < 300 {
			return nil, status, pager.Incomplete(i18n.T("the cluster page was not fully received", "страница кластеров получена не полностью"))
		}
		return nil, status, err
	}
	if len(data) == 0 {
		if message := resp.Header.Get("grpc-message"); message != "" {
			data = []byte(`{"message":` + strconv.Quote(message) + `}`)
		}
	}
	return data, status, nil
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
	return fmt.Errorf("dbaas API %d: %s", status, msg)
}

func (c *Client) listData(ctx context.Context, path string, query url.Values, token string) ([]byte, error) {
	q := url.Values{}
	for key, values := range query {
		q[key] = values
	}
	if token != "" {
		q.Set("page_token", token)
	}
	data, status, err := c.do(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return nil, err
	}
	if err := pager.PageStatus(status, data, token != "", apiError); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Client) getList(ctx context.Context, path string, query url.Values, field string) ([]map[string]any, error) {
	data, err := c.listData(ctx, path, query, "")
	if err != nil {
		return nil, err
	}
	// Kafka/Redis version and specification endpoints have no page_token parameter.
	items, err := pager.UnpagedObjects[map[string]any](data, field)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("parse %s: %w", "разбор %s: %w"), field, err)
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
		return nil, fmt.Errorf(i18n.T("parse operation: %w", "разбор операции: %w"), err)
	}
	return &op, nil
}

// maxListPages bounds the page loop against a server that never ends it.
const maxListPages = 50

// ListClusters lists clusters, following next_page_token. extra carries
// per-service query params (Kafka needs product_type=kafka; both take
// project_id — added here). A token already followed and more than
// maxListPages pages are errors: the list is not complete (break r4, R4-8;
// the cut list was a success).
func (c *Client) ListClusters(ctx context.Context, extra url.Values) ([]map[string]any, error) {
	items, _, err := c.ListClustersWithin(ctx, extra, maxListPages)
	return items, err
}

// ListClustersWithin returns a complete list and the number of pages requested.
// An incomplete list cannot establish membership in a project.
func (c *Client) ListClustersWithin(ctx context.Context, extra url.Values, limit int) ([]map[string]any, int, error) {
	limit = min(limit, maxListPages)
	q := url.Values{}
	for key, values := range extra {
		q[key] = values
	}
	if c.projectID != "" {
		q.Set("project_id", c.projectID)
	}
	// Kafka accepts at most 50 clusters per page.
	pageSize := 100
	if extra.Get("product_type") == "kafka" {
		pageSize = 50
	}
	q.Set("page_size", strconv.Itoa(pageSize))
	pages := 0
	var all []map[string]any
	var progress pager.ItemProgress
	more, err := pager.Upto(limit, func(token string) (string, error) {
		pages++
		data, err := c.listData(ctx, "/v1/clusters", q, token)
		if err != nil {
			return "", err
		}
		page, next, err := pager.ObjectPage[map[string]any](data, "clusters", "next_page_token")
		if err != nil {
			return "", fmt.Errorf(i18n.T("parse clusters: %w", "разбор кластеров: %w"), err)
		}
		if err := pager.CheckProgress(&progress, page, next, pager.ObjectID); err != nil {
			return "", err
		}
		all = append(all, page...)
		return next, nil
	})
	if err != nil {
		return nil, pages, pager.Named("dbaas: clusters", err)
	}
	if more {
		return nil, pages, pager.Named("dbaas: clusters", pager.TooManyPages(limit))
	}
	return all, pages, nil
}

// GetCluster returns one cluster by id.
func (c *Client) GetCluster(ctx context.Context, id string) (map[string]any, error) {
	return c.getObject(ctx, "/v1/clusters/"+url.PathEscape(id), url.Values{})
}

// CreateCluster creates a cluster (async). body is the full service-specific
// CreateClusterRequest (must include project_id + name).
func (c *Client) CreateCluster(ctx context.Context, body map[string]any) (*Operation, error) {
	return c.doOperation(ctx, http.MethodPost, "/v1/clusters", url.Values{}, body)
}

// DeleteCluster deletes a cluster by id (async).
func (c *Client) DeleteCluster(ctx context.Context, id string) (*Operation, error) {
	return c.doOperation(ctx, http.MethodDelete, "/v1/clusters/"+url.PathEscape(id), url.Values{}, nil)
}

// GetOperation polls an operation by id.
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

// ListVersions lists product versions. extra carries per-service query
// (Kafka needs product_type=kafka; Redis takes none).
func (c *Client) ListVersions(ctx context.Context, extra url.Values) ([]map[string]any, error) {
	return c.getList(ctx, "/v1/versions", extra, "versions")
}

// ListSpecifications lists flavor specs for a version in an availability zone.
// The API REQUIRES availability_zone_id (a UUID) despite the spec marking it
// optional: callers resolve one (tools.dbaasZone) instead of leaving it out.
func (c *Client) ListSpecifications(ctx context.Context, versionID, azID string) ([]map[string]any, error) {
	q := url.Values{}
	if versionID != "" {
		q.Set("version_id", versionID)
	}
	if azID != "" {
		q.Set("availability_zone_id", azID)
	}
	return c.getList(ctx, "/v1/specifications", q, "specifications")
}
