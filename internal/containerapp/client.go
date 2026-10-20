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
	"slices"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/pager"
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

// List returns the project's containers, every page (containers/openapi.yaml:
// nextPageToken in the answer, pageToken in the next request).
func (c *Client) List(ctx context.Context) ([]map[string]any, error) {
	var all []map[string]any
	err := pager.All("container-apps: containers", func(token string) (string, error) {
		var q url.Values
		if token != "" {
			q = url.Values{"pageToken": {token}}
		}
		data, st, err := c.do(ctx, http.MethodGet, "/v2/containers", q, nil)
		if err != nil {
			return "", err
		}
		if st != 200 {
			return "", apiError(st, data)
		}
		var r struct {
			Data          []map[string]any `json:"data"`
			NextPageToken string           `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", fmt.Errorf("parse containers: %w", err)
		}
		all = append(all, r.Data...)
		return r.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
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

// logsPageSize is the pageSize of an app logs request. The specification
// names the parameter but not its default nor the order of lines, and
// other Cloud.ru lists without pageSize answer an empty page with a new
// token (register AR-4) or 400 (certificate versions).
const logsPageSize = "100"

// logsMaxPages bounds the read of one app log: 50 pages of 100 lines.
// The console shows the last 1000 records (concepts__logging).
const logsMaxPages = 50

// LogsResult is the tail of a container's log.
type LogsResult struct {
	// Lines are the newest lines by time, oldest first.
	Lines []map[string]any
	// Read is how many lines the API returned in all pages.
	Read int
	// Cut reports that logsMaxPages pages were read and the API still
	// named a next page: older or newer lines (the order of the API is not
	// described) may be missing.
	Cut bool
}

// Logs returns the last tail lines of a container's log by time. App logs
// (GET /v2/containers/{name}/logs) are read page by page: nextPageToken in
// the answer, pageToken and pageSize in the request. The read ends on an
// empty page or a page already seen (a token the API ignores), as
// artifact.ListRegistries does. System logs (/systemLogs) have no
// pagination in the specification: one request.
func (c *Client) Logs(ctx context.Context, name string, system bool, tail int) (*LogsResult, error) {
	var all []map[string]any
	res := &LogsResult{}
	if system {
		data, err := c.logsPage(ctx, "/v2/containers/"+url.PathEscape(name)+"/systemLogs", nil)
		if err != nil {
			return nil, err
		}
		all = data.Data
	} else {
		p := "/v2/containers/" + url.PathEscape(name) + "/logs"
		seen := map[string]bool{}
		// StopOnRepeat: a log is a tail, not a list of resources, and the
		// read ends on an empty page or one already seen by decision of
		// #124 (CHANGELOG, ca_logs; register CAP-3: the order, the page
		// size and the end of the logs are not described). A cut read is
		// reported as truncated, nothing is deleted or counted from it.
		// The other lists treat a repeated token as an error (R4-6).
		more, err := pager.Upto(logsMaxPages, func(token string) (string, error) {
			q := url.Values{"pageSize": {logsPageSize}}
			if token != "" {
				q.Set("pageToken", token)
			}
			page, err := c.logsPage(ctx, p, q)
			if err != nil {
				return "", err
			}
			if len(page.Data) == 0 || seen[page.raw] {
				return "", nil
			}
			seen[page.raw] = true
			all = append(all, page.Data...)
			return page.NextPageToken, nil
		}, pager.StopOnRepeat)
		if err != nil {
			return nil, err
		}
		res.Cut = more
	}
	res.Read = len(all)
	res.Lines = lastByTime(all, tail, system)
	return res, nil
}

type logsPage struct {
	Data          []map[string]any `json:"data"`
	NextPageToken string           `json:"nextPageToken"`
	raw           string           // data as answered: a repeated page ends the read
}

func (c *Client) logsPage(ctx context.Context, path string, q url.Values) (*logsPage, error) {
	data, st, err := c.do(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var r logsPage
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse logs: %w", err)
	}
	if b, err := json.Marshal(r.Data); err == nil {
		r.raw = string(b)
	}
	return &r, nil
}

// lastByTime sorts lines oldest first and keeps the last tail of them. The
// time is timestamp (ContainerLog), for system logs lastTimestamp, else
// firstTimestamp (ContainerSystemLog). Lines without a time that parses
// sort first and keep the order of the API between them; tail <= 0 keeps
// all.
func lastByTime(lines []map[string]any, tail int, system bool) []map[string]any {
	keys := []string{"timestamp"}
	if system {
		keys = []string{"lastTimestamp", "firstTimestamp"}
	}
	at := func(m map[string]any) time.Time {
		for _, k := range keys {
			if s, ok := m[k].(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					return t
				}
			}
		}
		return time.Time{}
	}
	type line struct {
		t time.Time
		m map[string]any
	}
	ls := make([]line, len(lines))
	for i, m := range lines {
		ls[i] = line{at(m), m}
	}
	slices.SortStableFunc(ls, func(a, b line) int { return a.t.Compare(b.t) })
	if tail > 0 && len(ls) > tail {
		ls = ls[len(ls)-tail:]
	}
	out := make([]map[string]any, len(ls))
	for i, l := range ls {
		out[i] = l.m
	}
	return out
}
