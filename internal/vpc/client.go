// Package vpc is a client for the Cloud.ru Evolution VPC API
// (https://vpc.api.cloud.ru, /v1). IAM Bearer auth. VPC list/get is
// project-scoped via a project_id query; VPC & static-route create/delete are
// ASYNC → Operation, poll /v1/vpcs/operations/{id}. Security groups are NOT here
// (they live on the compute API). Route next-hops target a VM interface, a VIP,
// or a Magic Router.
package vpc

import (
	"bytes"
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

const defaultBaseURL = "https://vpc.api.cloud.ru"

// Client talks to the Evolution VPC v1 API.
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

// Operation is the async result of VPC/route create/delete.
type Operation struct {
	ID         string          `json:"id"`
	ResourceID string          `json:"resourceId,omitempty"`
	Done       bool            `json:"done"`
	Error      *OperationError `json:"error,omitempty"`
}

// OperationError is the VPC Status message.
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
		return nil, 0, fmt.Errorf("vpc request failed: %w", err)
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
	if status == http.StatusNotFound {
		// A VPC, route or operation that does not exist: NOT_FOUND (exit 4),
		// not API_ERROR (release audit, L-15).
		return clierrors.New(clierrors.CodeNotFound, fmt.Sprintf("vpc API %d: %s", status, msg),
			"Check the id: vpc_list lists the VPCs, vpc_routes the routes of one.")
	}
	return fmt.Errorf("vpc API %d: %s", status, msg)
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

// --- VPCs ---

// ListVPCs lists VPCs in the project.
func (c *Client) ListVPCs(ctx context.Context) ([]map[string]any, error) {
	q := url.Values{}
	if c.projectID != "" {
		q.Set("project_id", c.projectID)
	}
	data, status, err := c.do(ctx, http.MethodGet, "/v1/vpcs", q, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out struct {
		VPCs []map[string]any `json:"vpcs"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse vpcs: %w", err)
	}
	return out.VPCs, nil
}

// GetVPC returns one VPC by id.
func (c *Client) GetVPC(ctx context.Context, id string) (map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, "/v1/vpcs/"+url.PathEscape(id), url.Values{}, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse vpc: %w", err)
	}
	return out, nil
}

// CreateVPC creates a VPC (async). body: {projectId, name, description}.
func (c *Client) CreateVPC(ctx context.Context, body map[string]any) (*Operation, error) {
	return c.doOperation(ctx, http.MethodPost, "/v1/vpcs", body)
}

// DeleteVPC deletes a VPC by id (async).
func (c *Client) DeleteVPC(ctx context.Context, id string) (*Operation, error) {
	return c.doOperation(ctx, http.MethodDelete, "/v1/vpcs/"+url.PathEscape(id), nil)
}

// GetOperation polls a VPC async operation.
func (c *Client) GetOperation(ctx context.Context, id string) (*Operation, error) {
	data, status, err := c.do(ctx, http.MethodGet, "/v1/vpcs/operations/"+url.PathEscape(id), url.Values{}, nil)
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

// --- Static routes ---

// ListRoutes lists static routes of a VPC.
func (c *Client) ListRoutes(ctx context.Context, vpcID string) ([]map[string]any, error) {
	data, status, err := c.do(ctx, http.MethodGet, "/v1/vpcs/"+url.PathEscape(vpcID)+"/routes/static", url.Values{}, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	var out struct {
		Routes []map[string]any `json:"routes"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse routes: %w", err)
	}
	return out.Routes, nil
}

// CreateRoute creates a static route (async). body: {vpcId, projectId, subnet, nextHop}.
func (c *Client) CreateRoute(ctx context.Context, vpcID string, body map[string]any) (*Operation, error) {
	return c.doOperation(ctx, http.MethodPost, "/v1/vpcs/"+url.PathEscape(vpcID)+"/routes/static", body)
}

// DeleteRoute deletes a static route by id (async).
func (c *Client) DeleteRoute(ctx context.Context, vpcID, routeID string) (*Operation, error) {
	return c.doOperation(ctx, http.MethodDelete, "/v1/vpcs/"+url.PathEscape(vpcID)+"/routes/static/"+url.PathEscape(routeID), nil)
}
