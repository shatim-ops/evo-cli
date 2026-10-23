// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// faultAPI is the fault world of the services evo reaches over HTTP, the
// way faultCloud is compute's: a resource made stays made when the answer
// is lost. It keeps the collections of faultCollections — Managed Kafka,
// Redis and PostgreSQL clusters (the dbaas/v1 shape), Artifact Registry
// registries, compute's SNAT gateways — and answers the reads they need
// (zones, subnets, versions, specifications). failAt arms the nth request
// (1-based, every request counts, compute's included).
type faultAPI struct {
	mu     sync.Mutex
	seq    int
	log    []string
	n      int
	kind   string
	cancel context.CancelFunc

	made []faultResource
	next int
}

// faultResource is a billable resource of a collection: host and path of
// the collection, its id and name.
type faultResource struct {
	host, path, ID, Name string
}

// faultCollections are the collections faultAPI keeps, by path: the list
// field of their GET, and whether a POST answers an operation (the async
// services) or the resource itself (compute).
var faultCollections = map[string]struct {
	field string
	async bool
	// unique: the API refuses a second resource of the name, 409. Only
	// where it is known: a name in the path (containers, secrets), the
	// spec's 409 (disks, api-contract.md), a domain (DNS zones).
	unique bool
}{
	"/v1/clusters":         {field: "clusters", async: true},
	"/v1/registries":       {field: "registries", async: true},
	"/api/v1/nat-gateways": {field: "items"},
	"/v2/containers":       {field: "data", async: true, unique: true},
	"/v1/publicZones":      {field: "zones", async: true, unique: true},
	"/v1/vpcs":             {field: "vpcs", async: true},
	"/v2/secret":           {field: "secrets", unique: true},
	"/api/v1/disks":        {field: "items", unique: true},
}

func (a *faultAPI) failAt(n int, kind string) {
	a.mu.Lock()
	a.n, a.kind = n, kind
	a.mu.Unlock()
}

func (a *faultAPI) disarm() { a.failAt(0, "") }

func (a *faultAPI) callLog() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.log...)
}

func (a *faultAPI) billed() []faultBilled {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []faultBilled
	for _, c := range a.made {
		out = append(out, faultBilled{what: c.host + c.path + " " + c.Name, names: []string{c.ID}})
	}
	return out
}

func (a *faultAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	a.mu.Lock()
	a.seq++
	op := r.Method + " " + r.URL.Host + r.URL.Path
	a.log = append(a.log, op)
	kind := ""
	if a.n > 0 && a.seq == a.n {
		kind = a.kind
	}
	cancel := a.cancel
	a.mu.Unlock()

	reject := func(status int, msg string) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(status)
		_, _ = io.WriteString(rec, `{"message":"`+msg+`"}`)
		return rec.Result(), nil
	}
	switch kind {
	case faultCancel:
		if cancel != nil {
			cancel()
		}
		return nil, context.Canceled
	case faultTransport:
		return nil, errors.New("fault: connection refused")
	case faultAuth:
		return reject(401, "token expired")
	case faultReject:
		return reject(409, "already exists")
	case faultHTTP500:
		return reject(500, "upstream")
	}
	rec := httptest.NewRecorder()
	a.serve(rec, r)
	switch kind {
	case faultLost:
		return nil, errors.New("fault: connection reset after the request was served")
	case faultEmpty:
		if rec.Code >= 200 && rec.Code < 300 {
			rec = httptest.NewRecorder()
			_, _ = io.WriteString(rec, `{}`)
		}
	}
	return rec.Result(), nil
}

func (a *faultAPI) serve(w http.ResponseWriter, r *http.Request) {
	host, path := r.URL.Host, r.URL.Path
	switch {
	case path == "/api/v1/availability-zones":
		_, _ = io.WriteString(w, `{"items":[{"id":"`+zoneAZ1+`","name":"ru.AZ-1"}]}`)
	case path == "/api/v1/subnets":
		_, _ = io.WriteString(w, `{"items":[{"id":"sn-1","name":"Default_ru.AZ-1","default":true,"availability_zone":{"id":"`+zoneAZ1+`","name":"ru.AZ-1"}}],"total":1}`)
	case path == "/v1/versions":
		_, _ = io.WriteString(w, `{"versions":[{"id":"v1","name":"1"}]}`)
	case path == "/v1/specifications":
		_, _ = io.WriteString(w, `{"specifications":[{"id":"s-1","name":"small","cpu":2,"memory":4,"min_storage_gigs":40,"min_brokers":1}]}`)
	case faultCollections[path].field != "" && r.Method == http.MethodGet:
		a.mu.Lock()
		items := []map[string]any{}
		for _, c := range a.made {
			if c.host == host && c.path == path {
				items = append(items, map[string]any{"id": c.ID, "name": c.Name, "path": c.Name, "status": "RUNNING"})
			}
		}
		a.mu.Unlock()
		b, _ := json.Marshal(map[string]any{faultCollections[path].field: items, "total": len(items)})
		_, _ = w.Write(b)
	case faultCollections[path].field != "" && r.Method == http.MethodPost:
		var body struct {
			Name string `json:"name"`
			Path string `json:"path"` // a secret's name
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Name == "" {
			body.Name = body.Path
		}
		a.mu.Lock()
		if faultCollections[path].unique {
			for _, c := range a.made {
				if c.host == host && c.path == path && c.Name == body.Name {
					a.mu.Unlock()
					http.Error(w, `{"message":"already exists"}`, http.StatusConflict)
					return
				}
			}
		}
		a.next++
		c := faultResource{host: host, path: path, ID: fmt.Sprintf("00000000-0000-4000-8000-%012x", 0x500+a.next), Name: body.Name}
		a.made = append(a.made, c)
		op := fmt.Sprintf("op-%d", a.next)
		a.mu.Unlock()
		if faultCollections[path].async {
			// resource_id in dbaas/v1, resourceId in Artifact Registry.
			_, _ = io.WriteString(w, `{"id":"`+op+`","resource_id":"`+c.ID+`","resourceId":"`+c.ID+`","done":false}`)
		} else {
			obj := `{"id":"` + c.ID + `","name":"` + c.Name + `","status":"creating"}`
			if path == "/v2/secret" {
				obj = `{"secret":` + obj + `}` // Secret Manager nests it
			}
			_, _ = io.WriteString(w, obj)
		}
	case strings.HasPrefix(path, "/v1/operations/"):
		_, _ = io.WriteString(w, `{"id":"`+strings.TrimPrefix(path, "/v1/operations/")+`","done":true}`)
	default:
		http.Error(w, `{"message":"not found"}`, 404)
	}
}

// faultAPIRegistry is a registry whose every service, compute included,
// is the fault world a.
func faultAPIRegistry(t *testing.T, a *faultAPI) *Registry {
	t.Helper()
	faultToolsIsolate(t)
	old := http.DefaultTransport
	http.DefaultTransport = a
	t.Cleanup(func() { http.DefaultTransport = old })
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: a}, "")
	cloud.Backoff = time.Millisecond
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: t.TempDir(),
			Timings:  faultToolsTimings(),
		}, nil
	})
}

// A create whose answer was lost while the list fails too (two faults, out
// of the matrix's one): the answer says the cluster may exist, is billed,
// and where to find it — not a bare error (Qwen review of #212).
func TestLostCreate_ListFailedSaysWhereToLook(t *testing.T) {
	failed := func(context.Context) ([]map[string]any, error) { return nil, errors.New("list: timeout") }
	text, isErr := lostCreate(context.Background(), failed, "Kafka cluster", "cluster_id", "evo-k", nil, "kafka_get cluster_id", "kafka_list", errors.New("create cluster: lost"))
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || !isErr {
		t.Fatalf("answer %s err=%v isErr=%v", text, err, isErr)
	}
	if w, _ := m["warning"].(string); !strings.Contains(w, "kafka_list") || !strings.Contains(w, "evo-k") || !strings.Contains(w, "billed") {
		t.Errorf("warning %q: want the name, billed and kafka_list", w)
	}
	absent := func(context.Context) ([]map[string]any, error) { return nil, nil }
	if text, isErr := lostCreate(context.Background(), absent, "Kafka cluster", "cluster_id", "evo-k", nil, "kafka_get cluster_id", "kafka_list", errors.New("create cluster: 409")); !isErr || strings.Contains(text, "warning") {
		t.Errorf("listed and absent: %s; want the create's error only", text)
	}
}

// A create answered with success but no id is the API's fault: its answer
// says API_ERROR, not USAGE (the caller's arguments were fine).
func TestLostCreate_NoIDIsAPIError(t *testing.T) {
	absent := func(context.Context) ([]map[string]any, error) { return nil, nil }
	text, _ := lostCreate(context.Background(), absent, "SNAT gateway", "nat_gateway_id", "evo-n", nil, "snat_list", "snat_list", fmt.Errorf("create SNAT gateway: %w", noID("gateway")))
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || m["reason"] != "API_ERROR" {
		t.Errorf("reason %v, want API_ERROR: %s", m["reason"], text)
	}
}

// A resource found after a lost create may already have failed: the
// warning says its status, not only "creating" (Qwen review of #218).
func TestLostCreate_FoundSaysItsStatus(t *testing.T) {
	failed := func(context.Context) ([]map[string]any, error) {
		return []map[string]any{{"id": "c-1", "name": "evo-k", "status": "FAILED"}}, nil
	}
	text, isErr := lostCreate(context.Background(), failed, "Kafka cluster", "cluster_id", "evo-k", nil, "kafka_get cluster_id", "kafka_list", errors.New("create cluster: lost"))
	if isErr || !strings.Contains(text, "c-1") || !strings.Contains(text, "status FAILED") {
		t.Errorf("answer %s: want c-1 and its status FAILED", text)
	}
}
