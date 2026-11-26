package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testProject = "00000000-0000-4000-8000-000000000001"
	testZone    = "00000000-0000-4000-8000-000000000002"
	testID      = "00000000-0000-4000-8000-000000000003"
	otherID     = "00000000-0000-4000-8000-000000000004"
	testName    = "evo-g10-42-1"
	service404  = `[{"message":"not found"}]`
)

type computeStub struct {
	t                 *testing.T
	posts, deletes    int
	postID, marker    string
	postStatus        int
	afterDelete       string
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
			status = 404
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
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
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
		{"rate-limited", "", 429}, {"empty-id", "", 201}, {"malformed-id", "not-an-id", 201},
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
			if !k.r.PostSent || k.r.ID == testID {
				t.Error("receipt recovered a different ID from a list")
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
		{"compute", service404, true}, {"empty-array", "[]", false}, {"object", `{"message":"not found"}`, false},
		{"array-of-route-errors", `[{"message":"route not found"}]`, false}, {"gateway", `{"error_msg":"Not Found"}`, false}, {"grpc-route", `{"code":5,"message":"Not Found"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, w := newTestJob(t)
			w.afterDelete = tc.body
			if err := runTestJob(t, j, false); err != nil {
				t.Fatal(err)
			}
			k := reloadTestJob(t, j)
			err := runTestJob(t, k, true)
			if (err == nil) != tc.gone || k.r.Gone != tc.gone || w.deletes != 1 {
				t.Fatalf("wire DELETE %d gone=%t err=%v", w.deletes, k.r.Gone, err)
			}
			_ = runTestJob(t, reloadTestJob(t, k), true)
			if w.deletes != 1 {
				t.Errorf("duplicate cleanup sent %d DELETEs", w.deletes)
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
