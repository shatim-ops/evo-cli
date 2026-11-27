// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

// B-3 observes the JSON shape without writing resource values or page tokens.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/shatim-ops/evo-cli/internal/dbaas"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/postgresql"
)

const (
	iamURL   = "https://iam.api.cloud.ru/api/v1/auth/token"
	maxPages = 2 // per service; no continuation past this bound, no retries
	maxBody  = 8 << 20
)

type service struct {
	name, host string
	pageSize   int
}

var services = []service{
	{"postgresql", "postgresql.api.cloud.ru", 0}, // PG ListClusters leaves the page size to the service.
	{"kafka", "kafka.api.cloud.ru", 50},
	{"redis", "redis.api.cloud.ru", 100},
}

func (s service) query(project, cursor string) url.Values {
	q := url.Values{"project_id": {project}}
	if s.pageSize != 0 {
		q.Set("page_size", strconv.Itoa(s.pageSize))
	}
	if s.name == "kafka" {
		q.Set("product_type", "kafka")
	}
	if cursor != "" {
		q.Set("page_token", cursor)
	}
	return q
}

type response struct {
	status int
	data   []byte
	fault  string
}

func exchange(client *http.Client, req *http.Request, limit int64) response {
	resp, err := client.Do(req)
	if err != nil {
		return response{fault: "transport_error"}
	}
	defer resp.Body.Close()
	r := response{status: resp.StatusCode}
	if resp.StatusCode == http.StatusPartialContent || resp.Header.Get("Content-Range") != "" {
		r.fault = "partial_http"
		return r
	}
	r.data, err = io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(r.data)) > limit || (resp.ContentLength >= 0 && int64(len(r.data)) != resp.ContentLength) {
		r.data, r.fault = nil, "incomplete_body"
	}
	return r
}

func login(ctx context.Context, client *http.Client, key, secret string) string {
	b, _ := json.Marshal(map[string]string{"keyId": key, "secret": secret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, iamURL, bytes.NewReader(b))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	r := exchange(client, req, 64<<10)
	var t struct {
		Token string `json:"access_token"`
		Camel string `json:"accessToken"`
	}
	if r.fault != "" || r.status != 200 || json.Unmarshal(r.data, &t) != nil {
		return ""
	}
	if t.Token != "" {
		return t.Token
	}
	return t.Camel
}

type fetchPage func(context.Context, service, string) response

func remote(client *http.Client, project, token string) fetchPage {
	return func(ctx context.Context, s service, cursor string) response {
		u := url.URL{Scheme: "https", Host: s.host, Path: "/v1/clusters", RawQuery: s.query(project, cursor).Encode()}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return response{fault: "invalid_request"}
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		return exchange(client, req, maxBody)
	}
}

// Only the fixed vocabulary of JSON types, aggregate counts and local outcomes
// crosses the log boundary. The raw page and the next token stay in memory.
type pageShape struct {
	HTTP     int            `json:"http"`
	Body     string         `json:"body_type,omitempty"`
	Clusters string         `json:"clusters_type,omitempty"`
	Cursor   string         `json:"next_page_token_type,omitempty"`
	More     *bool          `json:"has_continuation,omitempty"`
	Count    *int           `json:"elements,omitempty"`
	Types    map[string]int `json:"element_types,omitempty"`
	Names    map[string]int `json:"name_types,omitempty"`
}
type check struct {
	Result string `json:"result"`
	Count  *int   `json:"elements,omitempty"`
}

type report struct {
	List      string      `json:"observed_list"`
	Service   string      `json:"service"`
	Pages     []pageShape `json:"pages"`
	Result    string      `json:"observation"`
	Candidate check       `json:"candidate_replay"`
}

func jsonType(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "missing"
	}
	switch raw[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 'n':
		return "null"
	case 'f', 't':
		return "boolean"
	default:
		return "number"
	}
}

func inspect(r response) (p pageShape, next, result string) {
	p.HTTP = r.status
	if r.fault != "" {
		return p, "", r.fault
	}
	if r.status != 200 {
		if json.Valid(r.data) {
			p.Body = jsonType(r.data)
		}
		return p, "", "http_error"
	}
	if !json.Valid(r.data) {
		p.Body = "invalid"
		return p, "", "invalid_json"
	}
	p.Body = jsonType(r.data)
	var rows json.RawMessage
	switch p.Body {
	case "object":
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(r.data, &obj)
		rows = obj["clusters"]
		p.Clusters, p.Cursor = jsonType(rows), jsonType(obj["next_page_token"])
		if p.Cursor == "string" {
			_ = json.Unmarshal(obj["next_page_token"], &next)
			more := next != ""
			p.More = &more
		}
	case "array":
		// A bare array is distinguishable from an object with clusters: [].
		p.Clusters, p.Cursor = "missing", "missing"
		rows = r.data
	default:
		return p, "", "unexpected_body"
	}
	if p.Body == "object" && p.Clusters == "missing" && next != "" {
		// An omitted repeated protobuf field can still carry a real cursor.
		// Preserve the missing count and request the next page within the cap.
		return p, next, ""
	}
	if jsonType(rows) != "array" {
		return p, "", "no_element_array"
	}
	var items []json.RawMessage
	_ = json.Unmarshal(rows, &items)
	n := len(items)
	p.Count = &n
	p.Types, p.Names = map[string]int{}, map[string]int{}
	for _, item := range items {
		kind := jsonType(item)
		p.Types[kind]++
		if kind == "object" {
			var object map[string]json.RawMessage
			_ = json.Unmarshal(item, &object)
			p.Names[jsonType(object["name"])]++
		}
	}
	if p.Types["object"] != n {
		return p, "", "unexpected_element"
	}
	if p.Cursor != "missing" && p.Cursor != "string" {
		return p, "", "unexpected_cursor"
	}
	return p, next, ""
}

type stored struct {
	cursor string
	body   []byte
}
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Replay the unchanged HTTP-200 pages using the product's public ListClusters
// entry point. No network, live credential or project ID is given to the client.
func replay(ctx context.Context, s service, tape []stored) check {
	const project = "local-scope"
	const host = "b3.invalid"
	old := httpretry.DefaultBase
	defer func() { httpretry.DefaultBase = old }()
	i := 0
	mismatch := false
	httpretry.DefaultBase = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if i >= len(tape) || req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != host || req.URL.Path != "/v1/clusters" || req.URL.RawQuery != s.query(project, tape[i].cursor).Encode() {
			mismatch = true
			return nil, errors.New("local request differs from captured sequence")
		}
		b := tape[i].body
		i++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b)), ContentLength: int64(len(b)), Request: req}, nil
	})
	var err error
	var items []map[string]any
	if s.name == "postgresql" {
		items, err = postgresql.New("", project, "https://"+host).ListClusters(ctx)
	} else {
		extra := url.Values{}
		if s.name == "kafka" {
			extra.Set("product_type", "kafka")
		}
		items, err = dbaas.New("", project, "https://"+host).ListClusters(ctx, extra)
	}
	if mismatch || (err == nil && i != len(tape)) {
		return check{Result: "request_mismatch"}
	}
	if err != nil {
		return check{Result: "rejected"}
	}
	n := len(items)
	return check{Result: "accepted", Count: &n}
}

func observe(ctx context.Context, s service, fetch fetchPage) report {
	out := report{Service: s.name, List: "unconfirmed", Candidate: check{Result: "not_replayed"}}
	var tape []stored
	seen := map[string]bool{"": true}
	cursor := ""
	count := 0
	missingArray := false
	for i := 0; i < maxPages; i++ {
		if ctx.Err() != nil {
			out.Result = "interrupted"
			return out
		}
		r := fetch(ctx, s, cursor)
		p, next, fault := inspect(r)
		out.Pages = append(out.Pages, p)
		if ctx.Err() != nil {
			out.Result = "interrupted"
			return out
		}
		if r.status == 200 && r.fault == "" {
			tape = append(tape, stored{cursor, r.data})
		}
		if fault != "" {
			out.Result = fault
			if len(tape) == len(out.Pages) {
				out.Candidate = replay(ctx, s, tape)
			}
			return out
		}
		if p.Count == nil {
			missingArray = true
		} else {
			count += *p.Count
		}
		if next == "" {
			out.Candidate = replay(ctx, s, tape)
			if count == 0 && missingArray {
				out.Result = "no_element_array"
				return out
			}
			out.Result = "complete"
			out.List = "empty"
			if count > 0 {
				out.List = "nonempty"
			}
			if out.Candidate.Count != nil && *out.Candidate.Count != count {
				out.Candidate.Result = "count_mismatch"
			}
			return out
		}
		if seen[next] {
			out.Result = "repeated_cursor"
			return out
		}
		seen[next] = true
		cursor = next
	}
	out.Result = "page_limit"
	return out
}

func newClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, DisableCompression: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func run() bool {
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 110*time.Second)
	defer cancel()
	key, secret, project := os.Getenv("CLOUDRU_KEY_ID"), os.Getenv("CLOUDRU_SECRET"), os.Getenv("CLOUDRU_PROJECT_ID")
	if key == "" || secret == "" || project == "" {
		fmt.Println("probe unavailable: missing setting")
		return false
	}
	client := newClient()
	token := login(ctx, client, key, secret)
	if token == "" {
		fmt.Println("probe unavailable: IAM exchange unconfirmed; list requests=0")
		return false
	}
	ok := true
	for _, s := range services {
		out := observe(ctx, s, remote(client, project, token))
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			return false
		}
		if out.Result != "complete" || out.Candidate.Result != "accepted" {
			ok = false
		}
	}
	return ok
}

func main() {
	if !run() {
		os.Exit(1)
	}
}
