package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testProject = "00000000-0000-4000-8000-000000000001"
	testZone    = "00000000-0000-4000-8000-000000000002"
	testID      = "00000000-0000-4000-8000-000000000003"
	otherID     = "00000000-0000-4000-8000-000000000004"
	testName    = "evo-g10-42-1"
	service404  = `[{"code":"floating_ip_not_found","message":"FloatingIp with this id is not found"}]`
)

type computeStub struct {
	t                 *testing.T
	posts, deletes    int
	postID, marker    string
	postStatus        int
	afterDelete       string
	afterStatus       int
	afterContentType  string
	afterContentRange string
	afterLengthExtra  int
	lists, finalCards int
	omitPOSTMarker    bool
	wrongCardMarker   bool
	wrongListMarker   bool
	cardChange        string
	afterBadIDListRow bool
	refusedDelete     bool
}

func newTestJob(t *testing.T) (*job, *computeStub) {
	t.Helper()
	w := &computeStub{t: t, postID: testID, postStatus: 201, afterDelete: service404}
	j := &job{r: receipt{Run: "42", Attempt: "1", Name: testName, Project: testProject, Zone: "ru.AZ-2"},
		path: filepath.Join(t.TempDir(), "receipt.json"), transport: w, interval: time.Millisecond}
	return j, w
}

// Each call uses only the real candidate adapter and this in-memory endpoint.
func runTestJob(t *testing.T, j *job, cleanup bool) error {
	t.Helper()
	a := j.adapter("fixture")
	a.Backoff = time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	if cleanup {
		return j.cleanup(ctx, a)
	}
	return j.probe(ctx, a)
}

func reloadTestJob(t *testing.T, j *job) *job {
	t.Helper()
	b, err := os.ReadFile(j.path)
	if err != nil {
		t.Fatal(err)
	}
	var r receipt
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return &job{r: r, path: j.path, transport: j.transport, interval: j.interval}
}

func (w *computeStub) resource(card bool) map[string]any {
	out := map[string]any{"id": testID, "name": testName, "state": "available", "description": w.marker,
		"availability_zone": map[string]any{"id": testZone, "name": "ru.AZ-2"}}
	if !card {
		if w.wrongListMarker {
			out["description"] = "list-changed"
		}
		return out
	}
	if w.wrongCardMarker {
		out["description"] = "card-changed"
	}
	switch w.cardChange {
	case "renamed":
		out["name"] = "someone-else"
	case "wrong-id":
		out["id"] = otherID
	case "wrong-zone":
		out["availability_zone"] = map[string]any{"id": otherID}
	case "interface", "vm", "nat_gateway":
		out[w.cardChange] = map[string]any{"id": otherID}
	}
	return out
}

func (w *computeStub) RoundTrip(r *http.Request) (*http.Response, error) {
	const collection = "/api/v1/floating-ips"
	status := 200
	var body string
	encode := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			w.t.Fatal(err)
		}
		return string(b)
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/v1/availability-zones":
		body = encode([]map[string]any{{"id": testZone, "name": "ru.AZ-2", "enabled": true}})
	case r.Method == "GET" && r.URL.Path == collection:
		w.lists++
		if r.URL.Query().Get("project_id") != testProject {
			w.t.Errorf("GET list used unexpected project")
		}
		rows := []map[string]any{}
		if w.posts > 0 && (w.postID == testID || w.afterBadIDListRow) && w.deletes == 0 {
			rows = append(rows, w.resource(false))
		}
		body = encode(map[string]any{"items": rows, "total": len(rows)})
	case r.Method == "POST" && r.URL.Path == collection:
		w.posts++
		var req struct {
			Name, Description string
			Project           string `json:"project_id"`
			Zone              string `json:"availability_zone_id"`
			Interface         string `json:"interface_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.t.Fatal(err)
		}
		if req.Name != testName || req.Zone != testZone || req.Project != testProject || req.Interface != "" || !strings.HasPrefix(req.Description, "evo-fip-") {
			w.t.Errorf("unexpected reservation name, scope, binding or attempt marker")
		}
		w.marker = req.Description
		status = w.postStatus
		if status == 429 {
			body = `{"message":"rate limited"}`
			break
		}
		out := w.resource(true)
		out["id"] = w.postID
		out["description"] = w.marker
		if w.omitPOSTMarker {
			delete(out, "description")
		}
		body = encode(out)
	case r.Method == "GET" && r.URL.Path == collection+"/"+testID:
		if w.deletes > 0 && !w.refusedDelete {
			w.finalCards++
			status = 404
			if w.afterStatus != 0 {
				status = w.afterStatus
			}
			body = w.afterDelete
		} else {
			body = encode(w.resource(true))
		}
	case r.Method == "DELETE" && r.URL.Path == collection+"/"+testID:
		if r.URL.Query().Get("project_id") != testProject {
			w.t.Errorf("DELETE used unexpected project")
		}
		w.deletes++
		if w.refusedDelete {
			status = 500
			body = `{"message":"uncertain delete"}`
		} else {
			status = 204
		}
	default:
		w.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		return nil, fmt.Errorf("unexpected request")
	}
	h := http.Header{"Content-Type": {"application/json"}, "Retry-After": {"0"}}
	length := int64(len(body))
	if w.deletes > 0 && r.Method == http.MethodGet && r.URL.Path == collection+"/"+testID {
		if w.afterContentType != "" {
			h.Set("Content-Type", w.afterContentType)
		}
		if w.afterContentRange != "" {
			h.Set("Content-Range", w.afterContentRange)
		}
		length += int64(w.afterLengthExtra)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), ContentLength: length, Request: r}, nil
}

func TestReceiptMarkerAndSingleWrites(t *testing.T) {
	j, w := newTestJob(t)
	if err := runTestJob(t, j, false); err != nil {
		t.Fatal(err)
	}
	k := reloadTestJob(t, j)
	if !k.r.PostSent || k.r.ID != testID || k.r.Marker != w.marker || !k.r.PostMarkerPresent || !k.r.PostMarkerMatch || !k.r.CardMarkerMatch || !k.r.ListMarkerMatch {
		t.Fatal("saved receipt does not match all three independently observed echoes")
	}
	_ = runTestJob(t, k, false)
	if w.posts != 1 {
		t.Fatalf("duplicate probe sent %d POSTs", w.posts)
	}
	if err := runTestJob(t, k, true); err != nil {
		t.Fatal(err)
	}
	k = reloadTestJob(t, k)
	if !k.r.DeleteSent || !k.r.Gone {
		t.Fatal("saved receipt does not record the confirmed removal")
	}
	_ = runTestJob(t, k, true)
	if w.deletes != 1 {
		t.Fatalf("duplicate cleanup sent %d DELETEs", w.deletes)
	}
}

func TestOnlyDirectPOSTIDCanAuthorizeCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		status   int
	}{
		{"rate-limited", "", 429}, {"empty-id", "", 201}, {"malformed-id", "not-an-id", 201}, {"nil-uuid", "00000000-0000-0000-0000-000000000000", 201},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, w := newTestJob(t)
			w.postStatus = tc.status
			w.postID = tc.id
			w.afterBadIDListRow = true
			if err := runTestJob(t, j, false); err == nil {
				t.Error("probe succeeded without a usable ID from its POST")
			}
			k := reloadTestJob(t, j)
			if !k.r.PostSent || k.r.ID != "" {
				t.Error("receipt retained an unusable ID or recovered one from a list")
			}
			_ = runTestJob(t, k, false)
			_ = runTestJob(t, k, true)
			if w.posts != 1 || w.deletes != 0 {
				t.Fatalf("wire writes: POST %d, DELETE %d", w.posts, w.deletes)
			}
		})
	}
}

func TestNegativeMarkerObservationStillCleansUp(t *testing.T) {
	for _, which := range []string{"POST", "GET", "list"} {
		t.Run(which, func(t *testing.T) {
			j, w := newTestJob(t)
			w.omitPOSTMarker = which == "POST"
			w.wrongCardMarker = which == "GET"
			w.wrongListMarker = which == "list"
			_ = runTestJob(t, j, false)
			k := reloadTestJob(t, j)
			if k.r.ID != testID || k.r.PostMarkerPresent != (which != "POST") || k.r.PostMarkerMatch != (which != "POST") || k.r.CardMarkerMatch != (which != "GET") || k.r.ListMarkerMatch != (which != "list") {
				t.Fatal("the three marker observations were not recorded independently")
			}
			if err := runTestJob(t, k, true); err != nil || w.posts != 1 || w.deletes != 1 || !k.r.Gone {
				t.Fatalf("cleanup of receipted free ID: POST=%d DELETE=%d gone=%t err=%v", w.posts, w.deletes, k.r.Gone, err)
			}
		})
	}
}

func TestChangedCardBlocksDelete(t *testing.T) {
	for _, change := range []string{"renamed", "wrong-id", "wrong-zone", "interface", "vm", "nat_gateway"} {
		t.Run(change, func(t *testing.T) {
			j, w := newTestJob(t)
			if err := runTestJob(t, j, false); err != nil {
				t.Fatal(err)
			}
			w.cardChange = change
			k := reloadTestJob(t, j)
			if err := runTestJob(t, k, true); err == nil || w.deletes != 0 || k.r.Gone {
				t.Fatalf("changed card: DELETE %d gone=%t err=%v", w.deletes, k.r.Gone, err)
			}
		})
	}
}

func TestGoneRequiresResource404(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		gone       bool
	}{
		{"documented example", service404, true},
		{"field order and whitespace", ` [ { "message":"FloatingIp with this id is not found", "code":"floating_ip_not_found" } ] `, true},
		{"generic service", `[{"message":"not found"}]`, false},
		{"project not found", `[{"message":"project not found"}]`, false},
		{"explicit project code", `[{"message":"FloatingIp with this id is not found","code":"project_not_found"}]`, false},
		{"contradictory message", `[{"message":"project not found","code":"floating_ip_not_found"}]`, false},
		{"permission failure", `[{"message":"not found","code":"auth_forbidden"}]`, false},
		{"conflicting entries", `[{"code":"floating_ip_not_found","message":"FloatingIp with this id is not found"},{"code":"auth_forbidden","message":"Forbidden"}]`, false},
		{"duplicate code", `[{"code":"project_not_found","code":"floating_ip_not_found","message":"FloatingIp with this id is not found"}]`, false},
		{"unknown field", `[{"code":"floating_ip_not_found","message":"FloatingIp with this id is not found","cause":"access denied"}]`, false},
		{"empty-array", "[]", false}, {"object", `{"message":"not found"}`, false},
		{"array of route errors", `[{"message":"route not found"}]`, false},
		{"gateway", `{"error_msg":"Not Found"}`, false}, {"grpc route", `{"code":5,"message":"Not Found"}`, false},
		{"authorization status", service404, false},
		{"wrong media type", service404, false},
		{"partial response", service404, false},
		{"truncated response", service404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, w := newTestJob(t)
			w.afterDelete = tc.body
			switch tc.name {
			case "authorization status":
				w.afterStatus = http.StatusForbidden
			case "wrong media type":
				w.afterContentType = "text/plain"
			case "partial response":
				w.afterContentRange = "bytes 0-20/50"
			case "truncated response":
				w.afterLengthExtra = 9
			}
			if err := runTestJob(t, j, false); err != nil {
				t.Fatal(err)
			}
			reads := w.lists
			k := reloadTestJob(t, j)
			err := runTestJob(t, k, true)
			if (err == nil) != tc.gone || k.r.Gone != tc.gone || w.posts != 1 || w.deletes != 1 || w.lists != reads || reads < 1 {
				t.Fatalf("final card: POST=%d DELETE=%d extra project lists=%d gone=%t err=%v", w.posts, w.deletes, w.lists-reads, k.r.Gone, err)
			}
			if w.afterStatus == 0 && w.finalCards != 1 {
				t.Fatalf("expected exactly one final GET on resource 404, got %d", w.finalCards)
			}
			retry := reloadTestJob(t, k)
			if retry.r.Gone != tc.gone || !retry.r.DeleteSent || retry.r.ID != testID {
				t.Fatalf("receipt lost its outcome or target")
			}
			again := runTestJob(t, retry, true)
			if (again == nil) != tc.gone || w.posts != 1 || w.deletes != 1 || w.lists != reads {
				t.Fatalf("retry: POST=%d DELETE=%d extra project lists=%d err=%v", w.posts, w.deletes, w.lists-reads, again)
			}
		})
	}
}

// Even a failed DELETE may have reached Compute. The next job reads the same
// receipt and card without sending a second mutation.
func TestUncertainDeleteSurvivesCleanupRetry(t *testing.T) {
	j, w := newTestJob(t)
	if err := runTestJob(t, j, false); err != nil {
		t.Fatal(err)
	}
	w.refusedDelete = true
	k := reloadTestJob(t, j)
	if err := runTestJob(t, k, true); err == nil || w.deletes != 1 || k.r.Gone {
		t.Fatalf("after uncertain DELETE: deletes=%d gone=%t err=%v", w.deletes, k.r.Gone, err)
	}
	k = reloadTestJob(t, k)
	if !k.r.DeleteSent {
		t.Fatal("missing persistent record of DELETE")
	}
	_ = runTestJob(t, k, true)
	if w.posts != 1 || w.deletes != 1 || k.r.Gone {
		t.Fatalf("retry: posts=%d deletes=%d gone=%t", w.posts, w.deletes, k.r.Gone)
	}
}

func TestMutationTransportDoesNotReplayLostResponses(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var count, proto atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				proto.Store(int32(r.ProtoMajor))
				_, _ = io.Copy(io.Discard, r.Body)
				// HTTP/1 closes the connection after the request reaches the server.
				panic(http.ErrAbortHandler)
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()

			transport := probeTransport()
			defer transport.CloseIdleConnections()
			transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			var body io.Reader
			if method == http.MethodPost {
				body = strings.NewReader("{}") // replayable request body, as in the adapter
			}
			req, err := http.NewRequest(method, server.URL, body)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				t.Fatal("expected a lost response")
			}
			if proto.Load() != 1 || count.Load() != 1 {
				t.Fatalf("lost %s response: HTTP/%d, %d requests received", method, proto.Load(), count.Load())
			}
		})
	}
}
