// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package secret is a client for the Cloud.ru Secret Management API v2
// (https://secretmanager.api.cloud.ru, /v2). IAM Bearer auth. The v2 API is
// project-scoped via a `projectId` query (camelCase — this gateway wants camel,
// unlike DNS which took snake) and addresses secrets by path (folder/name).
// NB: v1 (/v1/secrets) uses parentId = a SERVICE-INSTANCE id, not the project —
// passing project_id there 403s; v2 is the project-scoped API. Secret values are
// Base64 in the `payload` field.
package secret

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

const defaultBaseURL = "https://secretmanager.api.cloud.ru"

// Client talks to the Secret Management v2 API.
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

// escapePath escapes each path segment but keeps the folder "/" separators.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		switch seg {
		case ".", "..":
			// Not a folder name: the server would resolve it to
			// another secret than the one the caller named.
			parts[i] = strings.Repeat("%2E", len(seg))
		default:
			parts[i] = url.PathEscape(seg)
		}
	}
	return strings.Join(parts, "/")
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, int, error) {
	if query == nil {
		query = url.Values{}
	}
	if query.Get("projectId") == "" && c.projectID != "" {
		query.Set("projectId", c.projectID)
	}
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
		return nil, 0, fmt.Errorf("secret request failed: %w", err)
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
	return fmt.Errorf("secret API %d: %s", status, msg)
}

// List returns all secrets in the project (metadata only, no values),
// recursing into all folders (depth=-1 — otherwise the search returns only the
// root folder and misses secrets nested in folders like "folder/name").
func (c *Client) List(ctx context.Context) ([]map[string]any, error) {
	q := url.Values{}
	q.Set("depth", "-1")
	data, status, err := c.do(ctx, http.MethodGet, "/v2/secret", q, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out struct {
		Secrets []map[string]any `json:"secrets"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse secrets: %w", err)
	}
	return out.Secrets, nil
}

// Get returns one secret's metadata by path (no value).
func (c *Client) Get(ctx context.Context, path string) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, "/v2/secret/"+escapePath(path), nil, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse secret: %w", err)
	}
	// GET /v2/secret/{path} nests the secret under "secret" — unwrap it.
	if inner, ok := out["secret"].(map[string]any); ok {
		return inner, nil
	}
	return out, nil
}

// Create creates a secret. body: {projectId, path, payload(base64), ...}.
func (c *Client) Create(ctx context.Context, body map[string]any) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodPost, "/v2/secret", nil, body)
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

// AddVersion adds a new secret value version. body: {projectId, path, payload(base64)}.
func (c *Client) AddVersion(ctx context.Context, body map[string]any) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodPost, "/v2/version", nil, body)
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

// AccessPayload returns the Base64 payload of a secret version at path.
// version 0 → latest/primary.
func (c *Client) AccessPayload(ctx context.Context, path string, version int) (string, error) {
	q := url.Values{}
	if version > 0 {
		q.Set("version", fmt.Sprintf("%d", version))
	}
	data, status, err := c.do(ctx, http.MethodGet, "/v2/version/"+escapePath(path), q, nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", apiError(status, data)
	}
	var out struct {
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("parse payload: %w", err)
	}
	return out.Payload, nil
}

// Delete deletes a secret by path. force allows deleting an in-use secret.
func (c *Client) Delete(ctx context.Context, path string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", "true")
	}
	data, status, err := c.do(ctx, http.MethodDelete, "/v2/secret/"+escapePath(path), q, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return apiError(status, data)
	}
	return nil
}
