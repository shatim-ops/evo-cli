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

	"github.com/shatim-ops/evo-cli/internal/httpretry"
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
		return nil, 0, fmt.Errorf("dbaas request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	// grpc-gateway returns 4xx/5xx with an EMPTY body and the reason in the
	// grpc-message header — surface it (same pattern as postgresql).
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
	return fmt.Errorf("dbaas API %d: %s", status, msg)
}

func (c *Client) getList(ctx context.Context, path string, query url.Values, field string) ([]map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", field, err)
	}
	var items []map[string]any
	if raw, ok := out[field]; ok {
		_ = json.Unmarshal(raw, &items)
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
		return nil, fmt.Errorf("parse response: %w", err)
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
		return nil, fmt.Errorf("parse operation: %w", err)
	}
	return &op, nil
}

// listPageSize is the page_size of ListClusters. The specifications
// (openapi-kafka.yaml, openapi-redis.yaml) name no range; Redis rejects
// page_size<=0. cloud-e2e readonly #23 got API_ERROR from the Kafka and
// Redis lists with page_size=1000 (the HTTP status was not logged; an
// out-of-range page_size is a suspect, not a proven cause), so a page is
// kept to a common maximum and the rest is read by next_page_token, which
// the old code ignored.
const listPageSize = 100

// maxListPages bounds the page loop against a server that never ends it.
const maxListPages = 50

// ListClusters lists clusters, following next_page_token. extra carries
// per-service query params (Kafka needs product_type=kafka; both take
// project_id — added here).
func (c *Client) ListClusters(ctx context.Context, extra url.Values) ([]map[string]any, error) {
	var all []map[string]any
	seen := map[string]bool{}
	token := ""
	for i := 0; i < maxListPages; i++ {
		q := url.Values{}
		for k, v := range extra {
			q[k] = v
		}
		if c.projectID != "" {
			q.Set("project_id", c.projectID)
		}
		q.Set("page_size", strconv.Itoa(listPageSize))
		if token != "" {
			q.Set("page_token", token)
		}
		data, status, err := c.do(ctx, http.MethodGet, "/v1/clusters", q, nil)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, apiError(status, data)
		}
		var page struct {
			Clusters      []map[string]any `json:"clusters"`
			NextPageToken string           `json:"next_page_token"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("parse clusters: %w", err)
		}
		all = append(all, page.Clusters...)
		if page.NextPageToken == "" || seen[page.NextPageToken] {
			break
		}
		seen[page.NextPageToken] = true
		token = page.NextPageToken
	}
	return all, nil
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
		return nil, fmt.Errorf("parse operation: %w", err)
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
