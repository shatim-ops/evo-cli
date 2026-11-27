package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func listed(first, count, total int) reply {
	rows := make([]map[string]string, 0, count)
	for i := first; i < first+count; i++ {
		rows = append(rows, map[string]string{"id": fmt.Sprintf("fixture-id-%d", i), "name": fmt.Sprintf("fixture-name-%d", i), "type": "fixture-private-enum"})
	}
	body, _ := json.Marshal(map[string]any{"items": rows, "total": total})
	return reply{200, body, ""}
}

func TestBoundedWalk(t *testing.T) {
	for _, tc := range []struct {
		name              string
		total, serverPage int
		want              []string
		coverage          string
		overlap           int
	}{
		{"empty", 0, 100, []string{"project:100:0", "public:100:0"}, "complete", 0},
		{"two", 2, 100, []string{"project:100:0", "sample:1:0", "sample:1:1", "public:100:0"}, "complete", 2},
		{"five", 5, 100, []string{"project:100:0", "sample:1:0", "sample:1:1", "sample:1:2", "public:100:0"}, "complete", 2},
		{"actual_page_sizes", 5, 2, []string{"project:100:0", "project:100:2", "project:100:4", "public:100:0"}, "complete", 2},
		{"cap", 301, 100, []string{"project:100:0", "project:100:100", "project:100:200", "public:100:0"}, "page_cap", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			result := observe(func(scope string, limit, offset int) reply {
				calls = append(calls, fmt.Sprintf("%s:%d:%d", scope, limit, offset))
				if scope == "public" {
					return listed(0, 2, 3)
				} // deliberately only the first public page
				return listed(offset, min(tc.total-offset, limit, tc.serverPage), tc.total)
			})
			if !reflect.DeepEqual(calls, tc.want) || result.Coverage != tc.coverage || result.OK != (tc.coverage == "complete") || result.Overlap == nil || *result.Overlap != tc.overlap || *result.PublicNames != tc.overlap || result.Natural404 != "not_observed" {
				t.Fatalf("calls=%v result=%+v", calls, result)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "fixture-") || result.Pages[len(calls)-1].Types["other"] != 2 {
				t.Fatal("unsafe report or unsafe type label")
			}
		})
	}
}

func TestFailuresAreInconclusive(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  reply
		want string
	}{
		{"malformed", reply{200, []byte(`{"items":[`), ""}, "invalid_json"},
		{"duplicate_keys", reply{200, []byte(`{"items":[],"items":[],"total":0}`), ""}, "invalid_json"},
		{"null", reply{200, []byte("null"), ""}, "invalid_envelope"},
		{"bare", reply{200, []byte("[]"), ""}, "invalid_envelope"},
		{"wrong_items", reply{200, []byte(`{"items":null,"total":0}`), ""}, "invalid_items"},
		{"wrong_total", reply{200, []byte(`{"items":[],"total":"0"}`), ""}, "invalid_total"},
		{"missing_name", reply{200, []byte(`{"items":[{"id":"a"}],"total":1}`), ""}, "missing_identity"},
		{"duplicate_id", reply{200, []byte(`{"items":[{"id":"a","name":"x"},{"id":"a","name":"y"}],"total":2}`), ""}, "duplicate_id"},
		{"short", listed(0, 0, 2), "early_empty_page"},
		{"unexpected_offset", reply{200, []byte(`{"items":[],"total":0,"offset":1}`), ""}, "inconsistent_offset"},
		{"partial", reply{206, nil, "partial_http"}, "partial_http"},
		{"service", reply{404, []byte(`[{"message":"fixture-detail"}]`), ""}, "service_shape_404"},
		{"gateway", reply{404, []byte(`{"error_msg":"fixture-detail"}`), ""}, "gateway_shape_404"},
		{"bad_404", reply{404, []byte("null"), ""}, "unclassified_404"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := observe(func(scope string, limit, offset int) reply {
				if scope == "public" {
					return listed(0, 0, 0)
				}
				return tc.res
			})
			if r.OK || r.Pages[0].Fault != tc.want || len(r.Pages) != 2 || r.Overlap != nil || (r.Natural404 != "not_observed") != (tc.res.status == 404) {
				t.Fatalf("%+v; first=%+v", r, r.Pages[0])
			}
		})
	}
}

func TestChangedCatalogue(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second reply
		want          string
	}{
		{"total_changed", listed(0, 1, 2), listed(1, 1, 3), "changed_total"},
		{"repeated_page", listed(0, 1, 2), listed(0, 1, 2), "repeated_page_member"},
		{"sample_differs", listed(0, 3, 3), listed(10, 1, 3), "sample_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			r := observe(func(scope string, limit, offset int) reply {
				n++
				if scope == "public" {
					return listed(0, 0, 0)
				}
				if n == 1 {
					return tc.first
				}
				return tc.second
			})
			if r.OK || r.Coverage != "incomplete" || r.Pages[1].Fault != tc.want || (r.Stable == "changed") != (tc.want == "changed_total") {
				t.Fatalf("%+v; second=%+v", r, r.Pages[1])
			}
		})
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(body))}
}

func TestFixedWireAndNoSecretOutput(t *testing.T) {
	var calls []string
	client := &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == iamURL {
			if req.Method != "POST" || req.Header.Get("Authorization") != "" {
				t.Fatal("wrong IAM method or header")
			}
			var body map[string]string
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil || !reflect.DeepEqual(body, map[string]string{"keyId": "fixture-key", "secret": "fixture-secret"}) {
				t.Fatal("wrong IAM request")
			}
			return mockResponse(`{"access_token":"fixture-token"}`), nil
		}
		if req.Method != "GET" || req.URL.Scheme+"://"+req.URL.Host+req.URL.Path != imagesURL || req.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Fatal("unexpected catalogue request")
		}
		calls = append(calls, req.URL.RawQuery)
		q := req.URL.Query()
		if !q.Has("project_id") {
			return mockResponse(string(listed(0, 0, 0).data)), nil
		}
		if q.Get("limit") == "100" {
			return mockResponse(string(listed(0, 2, 2).data)), nil
		}
		offset := 0
		if q.Get("offset") == "1" {
			offset = 1
		}
		return mockResponse(string(listed(offset, 1, 2).data)), nil
	})}
	token := login(client, "fixture-key", "fixture-secret")
	r := observe(remote(client, "fixture-project", token))
	want := []string{"limit=100&project_id=fixture-project", "limit=1&offset=0&project_id=fixture-project", "limit=1&offset=1&project_id=fixture-project", "limit=100"}
	raw, _ := json.Marshal(r)
	if !r.OK || !reflect.DeepEqual(calls, want) || strings.Contains(string(raw), "fixture") {
		t.Fatalf("incorrect URL schedule or unsafe output")
	}
}

func TestRejectPartialTransport(t *testing.T) {
	for _, kind := range []string{"206", "range_header", "truncated_body"} {
		client := &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
			r := mockResponse(`{"items":[],"total":0}`)
			switch kind {
			case "206":
				r.StatusCode = 206
			case "range_header":
				r.Header.Set("Content-Range", "bytes 0-9/20")
			case "truncated_body":
				r.ContentLength++
			}
			return r, nil
		})}
		req, _ := http.NewRequest("GET", imagesURL, nil)
		if exchange(client, req).fault == "" {
			t.Fatal(kind)
		}
	}
}
