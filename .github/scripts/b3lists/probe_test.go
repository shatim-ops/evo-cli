// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func answer(req *http.Request, code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}

func TestShapeFromActualRequestsAndCandidateReplay(t *testing.T) {
	const cursor = "internal-token/+=?"
	const project = "private-project"
	const name = "private-resource-name"
	const id = "private-resource-id"
	const token = "private-bearer"
	for _, s := range services {
		t.Run(s.name, func(t *testing.T) {
			client := newClient()
			gets, iam := 0, 0
			client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost && r.URL.String() == iamURL {
					iam++
					if iam != 1 || r.Header.Get("Authorization") != "" {
						t.Fatal("unexpected authentication request")
					}
					return answer(r, 200, `{"access_token":"`+token+`"}`), nil
				}
				if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Hostname() != s.name+".api.cloud.ru" || r.URL.Path != "/v1/clusters" || r.Body != nil || r.Header.Get("Authorization") != "Bearer "+token {
					t.Fatal("a request escaped the selected collection")
				}
				q := url.Values{"project_id": {project}}
				switch s.name {
				case "postgresql":
				case "kafka":
					q.Set("product_type", "kafka")
					q.Set("page_size", "50")
				case "redis":
					q.Set("page_size", "100")
				default:
					t.Fatal("unknown service")
				}
				if gets == 1 {
					q.Set("page_token", cursor)
				}
				if gets >= 2 || !reflect.DeepEqual(r.URL.Query(), q) {
					t.Fatal("wrong parameters or extra GET")
				}
				gets++
				if gets == 1 {
					return answer(r, 200, `{"clusters":[],"next_page_token":"`+cursor+`"}`), nil
				}
				return answer(r, 200, `{"clusters":[{"name":"`+name+`","id":"`+id+`"}],"next_page_token":""}`), nil
			})
			ctx := t.Context()
			gotToken := login(ctx, client, "private-key", "private-secret")
			r := observe(ctx, s, remote(client, project, gotToken))
			if r.Result != "complete" || r.List != "nonempty" || r.Candidate.Result != "accepted" || r.Candidate.Count == nil || *r.Candidate.Count != 1 || gets != 2 || iam != 1 || len(r.Pages) != 2 || *r.Pages[0].Count != 0 || r.Pages[0].Cursor != "string" || r.Pages[1].Names["string"] != 1 {
				t.Fatalf("count or shape: %+v", r)
			}
			data, _ := json.Marshal(r)
			for _, bad := range []string{cursor, name, id, project, token, "private-key", "private-secret", "api.cloud.ru"} {
				if strings.Contains(string(data), bad) {
					t.Fatal("source data escaped into report")
				}
			}
		})
	}
}

func TestJSONShapesAndRawElementCount(t *testing.T) {
	for _, tc := range []struct {
		body, root, clusters, cursor, outcome string
		count                                 int
	}{
		{`{"clusters":[]}`, "object", "array", "missing", "", 0},
		{`{"clusters":[null]}`, "object", "array", "missing", "unexpected_element", 1},
		{`{"clusters":[{"name":false},{}]}`, "object", "array", "missing", "", 2},
		{`{"clusters":null}`, "object", "null", "missing", "no_element_array", -1},
		{`{}`, "object", "missing", "missing", "no_element_array", -1},
		{`{"clusters":[],"next_page_token":null}`, "object", "array", "null", "unexpected_cursor", 0},
		{`{"clusters":[],"next_page_token":7}`, "object", "array", "number", "unexpected_cursor", 0},
		{`[]`, "array", "missing", "missing", "", 0},
		{`null`, "null", "", "", "unexpected_body", -1},
		{`{"clusters":[`, "invalid", "", "", "invalid_json", -1},
	} {
		p, _, result := inspect(response{status: 200, data: []byte(tc.body)})
		n := -1
		if p.Count != nil {
			n = *p.Count
		}
		if p.Body != tc.root || p.Clusters != tc.clusters || p.Cursor != tc.cursor || n != tc.count || result != tc.outcome {
			t.Errorf("%s => %+v, %s", tc.body, p, result)
		}
	}
}

func TestNoExtraGetOrEmptyClaimWhenIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name     string
		statuses []int
		bodies   []string
		faults   []string
		want     string
	}{
		{"limit", []int{200, 200}, []string{`{"clusters":[],"next_page_token":"private1"}`, `{"clusters":[],"next_page_token":"private2"}`}, nil, "page_limit"},
		{"repeat", []int{200, 200}, []string{`{"clusters":[],"next_page_token":"private1"}`, `{"clusters":[],"next_page_token":"private1"}`}, nil, "repeated_cursor"},
		{"forbidden", []int{403}, []string{"private diagnostics"}, nil, "http_error"},
		{"partial", []int{206}, []string{"private diagnostics"}, []string{"partial_http"}, "partial_http"},
		{"second read interrupted", []int{200, 200}, []string{`{"clusters":[],"next_page_token":"private1"}`, ""}, []string{"", "incomplete_body"}, "incomplete_body"},
		{"transport", []int{0}, []string{""}, []string{"transport_error"}, "transport_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			r := observe(t.Context(), services[1], func(ctx context.Context, s service, cursor string) response {
				if n >= len(tc.bodies) {
					t.Fatal("read past the test case")
				}
				resp := response{status: tc.statuses[n], data: []byte(tc.bodies[n])}
				if tc.faults != nil {
					resp.fault = tc.faults[n]
				}
				n++
				return resp
			})
			if r.Result != tc.want || r.List != "unconfirmed" || r.Candidate.Result != "not_replayed" || n != len(tc.bodies) {
				t.Fatalf("%d GETs: %+v", n, r)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := observe(ctx, services[0], func(context.Context, service, string) response { t.Fatal("GET after cancellation"); return response{} })
	if r.Result != "interrupted" || len(r.Pages) != 0 {
		t.Fatalf("cancel: %+v", r)
	}
}

func TestTransportDoesNotRetryRedirectOrAcceptIncompleteBodies(t *testing.T) {
	for _, name := range []string{"redirect", "rate limit", "partial status", "range header", "short content length", "body limit", "read error"} {
		t.Run(name, func(t *testing.T) {
			c := newClient()
			calls := 0
			c.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls != 1 {
					t.Fatal("retried or followed a redirect")
				}
				resp := answer(r, 200, "data")
				switch name {
				case "redirect":
					resp.StatusCode = 307
					resp.Header.Set("Location", "https://elsewhere.invalid/")
				case "rate limit":
					resp.StatusCode = 429
				case "partial status":
					resp.StatusCode = 206
				case "range header":
					resp.Header.Set("Content-Range", "bytes 0-3/7")
				case "short content length":
					resp.ContentLength = 7
				case "read error":
					resp.Body = io.NopCloser(errReader{})
				}
				return resp, nil
			})
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.invalid", nil)
			limit := int64(8)
			if name == "body limit" {
				limit = 3
			}
			res := exchange(c, req, limit)
			if name == "redirect" || name == "rate limit" {
				if res.status == 200 || res.fault != "" {
					t.Fatal(res.status, res.fault)
				}
			} else if res.fault == "" || len(res.data) != 0 {
				t.Fatal("partial content passed")
			}
		})
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("private transport details") }

func TestEmptyResultRequiresAnObservedArray(t *testing.T) {
	for _, s := range services {
		for _, tc := range []struct{ label, body, want string }{
			{"observed array", `{"clusters":[],"next_page_token":""}`, "empty"},
			{"no array", `{}`, "unconfirmed"},
			{"null array", `{"clusters":null}`, "unconfirmed"},
			{"null cursor", `{"clusters":[],"next_page_token":null}`, "unconfirmed"},
			{"invalid json", `{"clusters":`, "unconfirmed"},
		} {
			t.Run(s.name+"/"+tc.label, func(t *testing.T) {
				out := observe(t.Context(), s, func(context.Context, service, string) response { return response{status: 200, data: []byte(tc.body)} })
				if out.List != tc.want || len(out.Pages) != 1 || out.Candidate.Result == "not_replayed" {
					t.Fatalf("wrong evidence: %+v", out)
				}
				if tc.want == "empty" && (out.Result != "complete" || out.Candidate.Count == nil || *out.Candidate.Count != 0 || out.Pages[0].More == nil || *out.Pages[0].More) {
					t.Fatalf("empty not confirmed: %+v", out)
				}
			})
		}
	}
}

func TestInterruptedReadCannotComplete(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := observe(ctx, services[0], func(context.Context, service, string) response {
		cancel()
		return response{status: 200, data: []byte(`{"clusters":[]}`)}
	})
	if r.Result != "interrupted" || r.List != "unconfirmed" || r.Candidate.Result != "not_replayed" {
		t.Fatalf("interrupted: %+v", r)
	}
}

func TestOmittedClustersPageCanContinueWithoutClaimingAnEmptyArray(t *testing.T) {
	for _, s := range services {
		for _, last := range []string{"nonempty", "empty", "missing", "more"} {
			t.Run(s.name+"/"+last, func(t *testing.T) {
				reads := 0
				r := observe(t.Context(), s, func(_ context.Context, _ service, cursor string) response {
					reads++
					if reads == 1 && cursor == "" {
						return response{status: 200, data: []byte(`{"next_page_token":"private-first"}`)}
					}
					if reads != 2 || cursor != "private-first" {
						t.Fatal("unexpected additional GET")
					}
					bodies := map[string]string{"nonempty": `{"clusters":[{"id":"sample","name":"sample"}]}`, "empty": `{"clusters":[]}`, "missing": `{}`, "more": `{"next_page_token":"private-next"}`}
					return response{status: 200, data: []byte(bodies[last])}
				})
				if reads != 2 || len(r.Pages) != 2 || r.Pages[0].Count != nil || r.Pages[0].Clusters != "missing" || r.Pages[0].More == nil || !*r.Pages[0].More {
					t.Fatalf("missing first array: %+v", r)
				}
				switch last {
				case "nonempty":
					if r.List != "nonempty" || r.Result != "complete" || r.Candidate.Result != "accepted" || r.Candidate.Count == nil || *r.Candidate.Count != 1 {
						t.Fatalf("nonempty second page: %+v", r)
					}
				case "empty", "missing":
					if r.List != "unconfirmed" || r.Result != "no_element_array" || r.Candidate.Result != "accepted" || r.Candidate.Count == nil || *r.Candidate.Count != 0 {
						t.Fatalf("raw absence vs candidate empty: %+v", r)
					}
				case "more":
					if r.List != "unconfirmed" || r.Result != "page_limit" || r.Candidate.Result != "not_replayed" {
						t.Fatalf("page limit: %+v", r)
					}
				}
			})
		}
	}
}
