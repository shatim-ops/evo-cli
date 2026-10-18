// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package containerapp is a small client for the Cloud.ru Container Apps API
// (https://containers.api.cloud.ru, /v2/containers). Serverless containers
// (Cloud-Run-like): projectId in query, IAM Bearer, create/delete are async.
package containerapp

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
)

const defaultBaseURL = "https://containers.api.cloud.ru"

// Client talks to the Container Apps v2 API.
type Client struct {
	http      *http.Client
	base      string
	token     string
	projectID string
}

func New(token, projectID, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{http: httpretry.NewClient(60 * time.Second), base: baseURL, token: token, projectID: projectID}
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
		return nil, 0, fmt.Errorf("container-apps request failed: %w", err)
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
	return fmt.Errorf("container-apps API %d: %s", status, msg)
}

// --- Types ---

type Operation struct {
	ID         string `json:"id"`
	ResourceID string `json:"resourceId,omitempty"`
	Done       bool   `json:"done"`
}

type Resources struct {
	CPU    string `json:"cpu"`    // e.g. "0.1"
	Memory string `json:"memory"` // e.g. "256Mi"
}

type Env struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"` // "secret" if secret
}

type Container struct {
	Name          string     `json:"name"`
	Image         string     `json:"image"`
	Resources     *Resources `json:"resources,omitempty"`
	ContainerPort int        `json:"containerPort,omitempty"`
	Env           []Env      `json:"env,omitempty"`
	Command       []string   `json:"command,omitempty"`
	Args          []string   `json:"args,omitempty"`
}

type Scaling struct {
	MinInstanceCount int `json:"minInstanceCount"`
	MaxInstanceCount int `json:"maxInstanceCount"`
}

type Template struct {
	Protocol    string      `json:"protocol,omitempty"`
	Timeout     string      `json:"timeout,omitempty"`
	IdleTimeout string      `json:"idleTimeout,omitempty"`
	Scaling     *Scaling    `json:"scaling,omitempty"`
	Containers  []Container `json:"containers"`
}

type Ingress struct {
	PubliclyAccessible bool `json:"publiclyAccessible"`
}

type Configuration struct {
	Ingress    *Ingress `json:"ingress,omitempty"`
	Privileged bool     `json:"privileged,omitempty"` // required to bind container ports <= 1024
}

type CreateContainerReq struct {
	ProjectID     string         `json:"projectId"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Configuration *Configuration `json:"configuration,omitempty"`
	Template      Template       `json:"template"`
}

// --- Operations ---

func (c *Client) List(ctx context.Context) ([]map[string]any, error) {
	data, st, err := c.do(ctx, http.MethodGet, "/v2/containers", nil, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var r struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse containers: %w", err)
	}
	return r.Data, nil
}

func (c *Client) Get(ctx context.Context, name string) (map[string]any, error) {
	data, st, err := c.do(ctx, http.MethodGet, "/v2/containers/"+url.PathEscape(name), nil, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse container: %w", err)
	}
	return m, nil
}

func (c *Client) Create(ctx context.Context, req CreateContainerReq) (*Operation, error) {
	if req.ProjectID == "" {
		req.ProjectID = c.projectID
	}
	data, st, err := c.do(ctx, http.MethodPost, "/v2/containers", nil, req)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf("parse operation: %w", err)
	}
	return &op, nil
}

func (c *Client) Delete(ctx context.Context, name string) (*Operation, error) {
	data, st, err := c.do(ctx, http.MethodDelete, "/v2/containers/"+url.PathEscape(name), nil, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var op Operation
	_ = json.Unmarshal(data, &op)
	return &op, nil
}

func (c *Client) StartStop(ctx context.Context, name, action string) error {
	data, st, err := c.do(ctx, http.MethodPost, "/v2/containers/"+url.PathEscape(name)+":"+action, nil, map[string]any{})
	if err != nil {
		return err
	}
	if st != 200 {
		return apiError(st, data)
	}
	return nil
}

func (c *Client) Logs(ctx context.Context, name string, system bool) ([]map[string]any, error) {
	p := "/v2/containers/" + url.PathEscape(name) + "/logs"
	if system {
		p = "/v2/containers/" + url.PathEscape(name) + "/systemLogs"
	}
	data, st, err := c.do(ctx, http.MethodGet, p, nil, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var r struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse logs: %w", err)
	}
	return r.Data, nil
}
