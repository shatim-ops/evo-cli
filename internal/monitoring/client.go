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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pager"
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
	return httpretry.ReadAPIResponse(req, resp, 0)
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
	if err := pager.PageStatus(status, data, false, apiError); err != nil {
		return nil, err
	}
	return parseSeries(data)
}

// parseSeries accepts an array, a wrapped array, or one JSON object per
// line. A broken line cannot turn a truncated export into successful history.
func parseSeries(data []byte) ([]Series, error) {
	data = bytes.TrimSpace(data)
	invalid := pager.Incomplete(i18n.T("invalid metrics export", "некорректный экспорт метрик"))
	var rows []json.RawMessage
	switch {
	case len(data) == 0:
		return nil, invalid
	case data[0] == '[':
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, invalid
		}
	case data[0] == '{':
		var wrap struct{ Series, Data, Result json.RawMessage }
		if json.Unmarshal(data, &wrap) == nil {
			// A standalone series (also the one-line streaming format).
			rows = []json.RawMessage{data}
			for _, raw := range []json.RawMessage{wrap.Series, wrap.Data, wrap.Result} {
				if raw == nil {
					continue
				}
				if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &rows) != nil {
					return nil, invalid
				}
				break
			}
		} else {
			for line := range bytes.SplitSeq(data, []byte("\n")) {
				if line = bytes.TrimSpace(line); len(line) != 0 {
					rows = append(rows, line)
				}
			}
		}
	default:
		return nil, invalid
	}
	out := make([]Series, len(rows))
	for i, raw := range rows {
		if !decodeSeries(raw, &out[i]) {
			return nil, invalid
		}
	}
	return out, nil
}

func decodeSeries(raw json.RawMessage, series *Series) bool {
	var row struct {
		Metric             map[string]*string
		Values, Timestamps json.RawMessage
	}
	if json.Unmarshal(raw, &row) != nil || row.Metric == nil ||
		json.Unmarshal(row.Values, &series.Values) != nil || json.Unmarshal(row.Timestamps, &series.Timestamps) != nil ||
		len(series.Values) != len(series.Timestamps) {
		return false
	}
	// encoding/json accepts null as a float64 or int64 zero. In a valid
	// numeric array the literal can occur only as a null sample. Check the
	// raw arrays so a large export needs no separate allocation per sample.
	if bytes.Contains(row.Values, []byte("null")) || bytes.Contains(row.Timestamps, []byte("null")) {
		return false
	}
	series.Metric = make(map[string]string, len(row.Metric))
	for key, value := range row.Metric {
		if value == nil {
			return false
		}
		series.Metric[key] = *value
	}
	return true
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
	if err := pager.PageStatus(status, data, false, apiError); err != nil {
		return "", err
	}
	return string(data), nil
}
