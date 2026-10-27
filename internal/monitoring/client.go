// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package monitoring is a client for the Cloud.ru Service Monitoring API
// (https://monitoring.api.cloud.ru). IAM Bearer auth, role monaas.metrics.reader
// or monaas.admin. Project-scoped in the PATH (…/v1/{projectId}/…), NOT a query.
// Two read surfaces (contract confirmed live — the API validated our requests,
// returning "tenant not found" only because a given project may not have a
// monitoring tenant provisioned yet):
//   - GET /v1/{projectId}/export_metrics?match=&start=&end=  — history, Unix ms,
//     max 7d / 30k series / 30k points. Returns {metric,values,timestamps}.
//   - GET /v2/{projectId}/scrape_metrics?match=              — current values in
//     Prometheus text exposition format (not JSON).
//
// `match` is a PromQL selector; export_metrics accepts a bare metric name.
package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

const defaultBaseURL = "https://monitoring.api.cloud.ru"

// Client talks to the Service Monitoring API.
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
	return &Client{http: httpretry.NewClient(60 * time.Second), base: strings.TrimRight(baseURL, "/"), token: token, projectID: projectID}
}

// ProjectID exposes the configured project.
func (c *Client) ProjectID() string { return c.projectID }

func (c *Client) do(ctx context.Context, path string, q url.Values) ([]byte, int, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf(i18n.T("monitoring request failed: %w", "запрос к monitoring не удался: %w"), err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func apiError(status int, body []byte) error {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	msg := e.Error
	if msg == "" {
		msg = e.Message
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("monitoring API %d: %s", status, msg)
}

// Series is one metric time series returned by export_metrics.
type Series struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

// ExportMetrics returns metric history over [startMs, endMs] (Unix milliseconds,
// max 7-day span). match is a PromQL name selector (bare metric name for the
// history endpoint). Response is one JSON object per series (JSON lines) or an
// array; we tolerate both.
func (c *Client) ExportMetrics(ctx context.Context, match string, startMs, endMs int64) ([]Series, error) {
	q := url.Values{}
	q.Set("match", match)
	q.Set("start", fmt.Sprintf("%d", startMs))
	q.Set("end", fmt.Sprintf("%d", endMs))
	data, status, err := c.do(ctx, "/v1/"+url.PathEscape(c.projectID)+"/export_metrics", q)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	return parseSeries(data), nil
}

// parseSeries tolerates a JSON array, a top-level {series|data|result:[…]}, or
// newline-delimited JSON objects (Prometheus-style streaming export).
func parseSeries(data []byte) []Series {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	if trimmed[0] == '[' {
		var arr []Series
		if json.Unmarshal(data, &arr) == nil {
			return arr
		}
	}
	if trimmed[0] == '{' {
		var wrap struct {
			Series []Series `json:"series"`
			Data   []Series `json:"data"`
			Result []Series `json:"result"`
		}
		if json.Unmarshal(data, &wrap) == nil {
			if len(wrap.Series) > 0 {
				return wrap.Series
			}
			if len(wrap.Data) > 0 {
				return wrap.Data
			}
			if len(wrap.Result) > 0 {
				return wrap.Result
			}
		}
	}
	// newline-delimited objects
	var out []Series
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s Series
		if json.Unmarshal([]byte(line), &s) == nil && (len(s.Values) > 0 || len(s.Metric) > 0) {
			out = append(out, s)
		}
	}
	return out
}

// ScrapeMetrics returns current metric values in Prometheus text exposition
// format (raw text — the caller renders/greps it). match is a PromQL selector.
func (c *Client) ScrapeMetrics(ctx context.Context, match string) (string, error) {
	q := url.Values{}
	if match != "" {
		q.Set("match", match)
	}
	data, status, err := c.do(ctx, "/v2/"+url.PathEscape(c.projectID)+"/scrape_metrics", q)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", apiError(status, data)
	}
	return string(data), nil
}
