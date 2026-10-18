// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package dns is a client for the Cloud.ru Evolution DNS API
// (https://dns.api.cloud.ru, /v1). IAM Bearer auth. Zones are split into
// publicZones/privateZones; records are TOP-LEVEL resources keyed by zone id:
// publicRecordsSole (?publicZoneId=) / privateRecordsSole (?privateZoneId=).
// Zone & record create/delete are ASYNC → dnsv1Operation, poll /v1/operations/{id}.
// NB: the REAL service API is dns.api.cloud.ru — the console-internal
// console.cloud.ru/api/clouddns rejects service-account tokens ("invalid scopes").
package dns

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

const defaultBaseURL = "https://dns.api.cloud.ru"

// Client talks to the Evolution DNS v1 API.
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

// Operation is the async result of zone/record create/delete.
type Operation struct {
	ID         string          `json:"id"`
	ResourceID string          `json:"resource_id,omitempty"`
	Done       bool            `json:"done"`
	Error      *OperationError `json:"error,omitempty"`
}

// UnmarshalJSON also reads resourceId, the name in the spec: with only
// resource_id, dns_zone_create could not return the new zone's id
// (api-contract §4.10).
func (o *Operation) UnmarshalJSON(b []byte) error {
	type plain Operation
	var r struct {
		plain
		ResourceIDCamel string `json:"resourceId"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*o = Operation(r.plain)
	if o.ResourceID == "" {
		o.ResourceID = r.ResourceIDCamel
	}
	return nil
}

// OperationError is rpcStatus.
type OperationError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// zoneResource maps visibility → zone path segment.
func zoneResource(visibility string) string {
	if visibility == "private" {
		return "privateZones"
	}
	return "publicZones"
}

// recordResource maps visibility → record path segment + zone-id field name.
// snake_case is used because this grpc-gateway is proven to accept it (zone list
// with project_id returns 200; field violations are reported in snake_case).
func recordResource(visibility string) (path, zoneParam string) {
	if visibility == "private" {
		return "privateRecordsSole", "private_zone_id"
	}
	return "publicRecordsSole", "public_zone_id"
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
		return nil, 0, fmt.Errorf("dns request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// APIError is a non-2xx answer of the DNS API: its HTTP status and message.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("dns API %d: %s", e.Status, e.Message) }

func apiError(status int, body []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	msg := e.Message
	if msg == "" {
		msg = string(body)
	}
	return &APIError{Status: status, Message: msg}
}

// getList reads every page of a list: the answer carries nextPageToken
// (dns/openapi.yaml), the next request sends it back as pageToken. A list
// without that field is one page.
func (c *Client) getList(ctx context.Context, path string, query url.Values, field string) ([]map[string]any, error) {
	var items []map[string]any
	err := pager.All("dns: "+field, func(token string) (string, error) {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		if token != "" {
			q.Set("pageToken", token)
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
			return "", fmt.Errorf("parse %s: %w", field, err)
		}
		var page []map[string]any
		if raw, ok := out[field]; ok {
			_ = json.Unmarshal(raw, &page)
		}
		items = append(items, page...)
		var next string
		if raw, ok := out["nextPageToken"]; ok {
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
		return nil, fmt.Errorf("parse response: %w", err)
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
		return nil, fmt.Errorf("parse operation: %w", err)
	}
	return &op, nil
}

// --- Zones ---

func (c *Client) ListZones(ctx context.Context, visibility string) ([]map[string]any, error) {
	q := url.Values{}
	if c.projectID != "" {
		q.Set("project_id", c.projectID)
	}
	return c.getList(ctx, "/v1/"+zoneResource(visibility), q, "zones")
}

func (c *Client) GetZone(ctx context.Context, visibility, id string) (map[string]any, error) {
	return c.getObject(ctx, "/v1/"+zoneResource(visibility)+"/"+url.PathEscape(id))
}

// CreateZone creates a zone (async). body must include projectId + domain.
func (c *Client) CreateZone(ctx context.Context, visibility string, body map[string]any) (*Operation, error) {
	return c.doOperation(ctx, http.MethodPost, "/v1/"+zoneResource(visibility), body)
}

// DeleteZone deletes a zone by id (async).
func (c *Client) DeleteZone(ctx context.Context, visibility, id string) (*Operation, error) {
	return c.doOperation(ctx, http.MethodDelete, "/v1/"+zoneResource(visibility)+"/"+url.PathEscape(id), nil)
}

// --- Records ---

func (c *Client) ListRecords(ctx context.Context, visibility, zoneID string) ([]map[string]any, error) {
	path, zoneParam := recordResource(visibility)
	q := url.Values{}
	q.Set(zoneParam, zoneID)
	return c.getList(ctx, "/v1/"+path, q, "records")
}

func (c *Client) GetRecord(ctx context.Context, visibility, recordID string) (map[string]any, error) {
	path, _ := recordResource(visibility)
	return c.getObject(ctx, "/v1/"+path+"/"+url.PathEscape(recordID))
}

// CreateRecord creates a record (async). body must include the zone-id field,
// name, type, values, ttl.
func (c *Client) CreateRecord(ctx context.Context, visibility string, body map[string]any) (*Operation, error) {
	path, _ := recordResource(visibility)
	return c.doOperation(ctx, http.MethodPost, "/v1/"+path, body)
}

// DeleteRecord deletes a record by id (async).
func (c *Client) DeleteRecord(ctx context.Context, visibility, recordID string) (*Operation, error) {
	path, _ := recordResource(visibility)
	return c.doOperation(ctx, http.MethodDelete, "/v1/"+path+"/"+url.PathEscape(recordID), nil)
}

// ZoneIDParam returns the zone-id body/query field name for a visibility.
func ZoneIDParam(visibility string) string {
	_, p := recordResource(visibility)
	return p
}

// --- Operations ---

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
