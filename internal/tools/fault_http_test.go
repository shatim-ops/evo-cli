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
	"slices"
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
// registries, compute's SNAT gateways, Kubernetes clusters, load balancers
// and the rest of the list below — and answers the reads they need (zones,
// subnets, versions, specifications, mk8s product configuration, a target
// group). failAt arms the nth request
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
	// Type is a DNS record's type.
	Type string
	// before: the world held it before the call; not the call's to name.
	before bool
}

// faultCollections are the collections faultAPI keeps, by path: the list
// field of their GET, and whether a POST answers an operation (the async
// services) or the resource itself (compute).
var faultCollections = map[string]struct {
	field string
	async bool
	// unique: the API refuses a second resource of the name, 409. Only
	// where it is known: a name in the path (containers, secrets), the
	// spec's 409 (disks and images, api-contract.md), a domain (DNS zones).
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
	"/api/v1/images":       {field: "items", unique: true}, // the spec's 409 «образ уже существует»
	// mk8s and NLB: their specs document no refusal of a taken name (D21).
	"/v3/clusters":             {field: "clusters", async: true},
	"/v2/networkLoadBalancers": {field: "balancers", async: true},
	"/v3/nodePools":            {field: "nodePools", async: true},
	"/v2/targetGroups":         {field: "target_groups", async: true},
	"/v1/publicRecordsSole":    {field: "records", async: true},
	"/v1/certificate":          {field: "certificates"},
	// Static routes of the VPC vpc_route_create adds to, known by their
	// destination (subnet).
	"/v1/vpcs/" + faultVPC + "/routes/static": {field: "routes", async: true},
	// Manual backups of the cluster pg_backup_create backs up.
	"/v1/clusters/" + faultPGCluster + "/backups": {field: "backups", async: true},
}

// faultPGCluster is the PostgreSQL cluster of pg_backup_create, faultVPC
// the VPC of vpc_route_create.
const (
	faultPGCluster = "00000000-0000-4000-8000-0000000000f2"
	faultVPC       = "00000000-0000-4000-8000-0000000000f5"
)

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
		if !c.before {
			out = append(out, faultBilled{what: c.host + c.path + " " + c.Name, names: []string{c.ID}})
		}
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
	case faultSlow:
		// http.Client reports its Timeout as the context's deadline.
		return nil, context.DeadlineExceeded
	case faultEmpty:
		if rec.Code >= 200 && rec.Code < 300 {
			rec = httptest.NewRecorder()
			_, _ = io.WriteString(rec, `{}`)
		}
	}
	return rec.Result(), nil
}

// faultS3Host is Object Storage's endpoint, path-style: /bucket and
// /bucket/key.
const faultS3Host = "s3.cloud.ru"

// serveS3 keeps buckets and objects: a bucket made stays made, an object
// put again is the same object.
func (a *faultAPI) serveS3(w http.ResponseWriter, r *http.Request) {
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	a.mu.Lock()
	defer a.mu.Unlock()
	find := func(path, id string) bool {
		for _, c := range a.made {
			if c.host == faultS3Host && c.path == path && c.ID == id {
				return true
			}
		}
		return false
	}
	switch {
	case bucket == "" && r.Method == http.MethodGet:
		var b strings.Builder
		b.WriteString(`<ListAllMyBucketsResult><Buckets>`)
		for _, c := range a.made {
			if c.host == faultS3Host && c.path == "bucket" {
				b.WriteString(`<Bucket><Name>` + c.ID + `</Name><CreationDate>2026-01-01T00:00:00.000Z</CreationDate></Bucket>`)
			}
		}
		b.WriteString(`</Buckets></ListAllMyBucketsResult>`)
		_, _ = io.WriteString(w, b.String())
	case key == "" && r.Method == http.MethodGet && r.URL.Query().Has("location"):
		_, _ = io.WriteString(w, `<LocationConstraint>ru-central-1</LocationConstraint>`)
	case key == "" && r.Method == http.MethodGet && find("bucket", bucket):
		// ListObjectsV2 by prefix, as s3_rm looks for the key it deletes.
		prefix := r.URL.Query().Get("prefix")
		var b strings.Builder
		b.WriteString(`<ListBucketResult><Name>` + bucket + `</Name><IsTruncated>false</IsTruncated>`)
		for _, c := range a.made {
			if c.host == faultS3Host && c.path == "object "+bucket && strings.HasPrefix(c.ID, prefix) {
				b.WriteString(`<Contents><Key>` + c.ID + `</Key><Size>2</Size><LastModified>2026-01-01T00:00:00.000Z</LastModified></Contents>`)
			}
		}
		b.WriteString(`</ListBucketResult>`)
		_, _ = io.WriteString(w, b.String())
	case key == "" && r.Method == http.MethodHead:
		if !find("bucket", bucket) {
			w.WriteHeader(http.StatusNotFound)
		}
	case key == "" && r.Method == http.MethodPut:
		if find("bucket", bucket) {
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `<Error><Code>BucketAlreadyOwnedByYou</Code><Message>owned by you</Message></Error>`)
			return
		}
		a.made = append(a.made, faultResource{host: faultS3Host, path: "bucket", ID: bucket, Name: bucket})
	case key != "" && r.Method == http.MethodPut:
		if !find("bucket", bucket) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<Error><Code>NoSuchBucket</Code><Message>no bucket</Message></Error>`)
			return
		}
		// An object is named by its key; the collection is its bucket.
		if !find("object "+bucket, key) {
			a.made = append(a.made, faultResource{host: faultS3Host, path: "object " + bucket, ID: key, Name: key})
		}
		w.Header().Set("ETag", `"0"`)
	case key != "" && r.Method == http.MethodDelete:
		// S3 answers 204 to a key that is not there as to one that is.
		for k, c := range a.made {
			if c.host == faultS3Host && c.path == "object "+bucket && c.ID == key {
				a.made = append(a.made[:k], a.made[k+1:]...)
				break
			}
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code><Message>no such path</Message></Error>`)
	}
}

func (a *faultAPI) serve(w http.ResponseWriter, r *http.Request) {
	host, path := r.URL.Host, r.URL.Path
	switch {
	case host == faultS3Host:
		a.serveS3(w, r)
	case path == "/api/v1/availability-zones":
		_, _ = io.WriteString(w, `{"items":[{"id":"`+zoneAZ1+`","name":"ru.AZ-1"}]}`)
	case path == "/api/v1/subnets":
		_, _ = io.WriteString(w, `{"items":[{"id":"sn-1","name":"Default_ru.AZ-1","default":true,"vpc_id":"vpc-1","availability_zone_id":"`+zoneAZ1+`","availability_zone":{"id":"`+zoneAZ1+`","name":"ru.AZ-1"}}],"total":1}`)
	case path == "/v1/versions":
		_, _ = io.WriteString(w, `{"versions":[{"id":"v1","name":"1"}]}`)
	case path == "/v3/productConfiguration":
		_, _ = io.WriteString(w, `{"defaultZone":"ru.AZ-1","zones":[{"zoneId":"mk-az1","name":"ru.AZ-1"}],
			"flavors":[{"flavorId":"fl-1","name":"gen-2-4","cpu":2,"ram":4}],
			"channels":[{"channel":"STABLE","availableVersions":["v1.34.6"]}]}`)
	case strings.HasPrefix(path, "/v3/clusters/"):
		_, _ = io.WriteString(w, `{"id":"`+strings.TrimPrefix(path, "/v3/clusters/")+`","controlPlane":{"zones":["mk-az1"]}}`)
	case path == "/api/v1/security-groups":
		_, _ = io.WriteString(w, `{"items":[{"id":"sg-1","name":"SSH-access_ru.AZ-1","availability_zone":{"id":"`+zoneAZ1+`","name":"ru.AZ-1"}}],"total":1}`)
	case path == "/v2/targetGroups/tg-1":
		_, _ = io.WriteString(w, `{"id":"tg-1","name":"tg","vpc_id":"vpc-1","target_vms":[]}`)
	case path == "/v1/specifications":
		_, _ = io.WriteString(w, `{"specifications":[{"id":"s-1","name":"small","cpu":2,"memory":4,"min_storage_gigs":40,"min_brokers":1}]}`)
	case faultCollections[path].field != "" && r.Method == http.MethodGet:
		a.mu.Lock()
		items := []map[string]any{}
		for _, c := range a.made {
			if c.host == host && c.path == path {
				items = append(items, map[string]any{"id": c.ID, "name": c.Name, "path": c.Name, "subnet": c.Name, "type": strings.ToUpper(c.Type), "status": "RUNNING"})
			}
		}
		a.mu.Unlock()
		b, _ := json.Marshal(map[string]any{faultCollections[path].field: items, "total": len(items)})
		_, _ = w.Write(b)
	case faultCollections[path].field != "" && r.Method == http.MethodPost:
		var body struct {
			Name   string `json:"name"`
			Path   string `json:"path"`   // a secret's name
			Subnet string `json:"subnet"` // a static route's destination
			Type   string `json:"type"`   // a DNS record's type
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Name == "" {
			body.Name = body.Path + body.Subnet
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
		c := faultResource{host: host, path: path, ID: fmt.Sprintf("00000000-0000-4000-8000-%012x", 0x500+a.next), Name: body.Name, Type: body.Type}
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
	case path == "/v1/certificate:issue_lenc_cert" && r.Method == http.MethodPost:
		// Let's Encrypt: the certificate is in the list at once, the
		// answer is an operation.
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		a.mu.Lock()
		a.next++
		a.made = append(a.made, faultResource{host: host, path: "/v1/certificate", ID: fmt.Sprintf("00000000-0000-4000-8000-%012x", 0x500+a.next), Name: body.Name})
		op := fmt.Sprintf("op-%d", a.next)
		a.mu.Unlock()
		_, _ = io.WriteString(w, `{"id":"`+op+`","done":false}`)
	case strings.HasPrefix(path, "/v1/operations/"):
		_, _ = io.WriteString(w, `{"id":"`+strings.TrimPrefix(path, "/v1/operations/")+`","done":true}`)
	case r.Method == http.MethodDelete && a.remove(host, path):
		_, _ = io.WriteString(w, `{}`)
	default:
		http.Error(w, `{"message":"not found"}`, 404)
	}
}

// remove deletes the resource a DELETE of path names: an item of the
// longest collection the path is under, by its id or name — a name may
// hold "/" (a secret's path, app/db). It reports whether there was one; a
// DELETE of what is not there is 404.
func (a *faultAPI) remove(host, path string) bool {
	coll := ""
	for c := range faultCollections {
		if strings.HasPrefix(path, c+"/") && len(c) > len(coll) {
			coll = c
		}
	}
	if coll == "" {
		return false
	}
	ref := strings.TrimPrefix(path, coll+"/")
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, c := range a.made {
		if c.host == host && c.path == coll && (c.ID == ref || c.Name == ref) {
			a.made = append(a.made[:k], a.made[k+1:]...)
			return true
		}
	}
	return false
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
			Config: config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s",
				S3: config.S3Config{AccessKey: "fault-access", SecretKey: "fault-secret"}},
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
	// A script that reads status alone must not wait for a failed one to
	// come up (Qwen review of #230).
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || m["status"] != "failed" {
		t.Errorf("status %v, want failed: %s", m["status"], text)
	}
}

// A backup of the name in the cluster is refused before the consent, with
// its id: a repeat after a lost answer made a second backup (fault matrix,
// process audit 1.2.1).
func TestPgBackupCreate_TakenNameRefusedBeforeConsent(t *testing.T) {
	a := &faultAPI{made: []faultResource{{host: "postgresql.api.cloud.ru", path: "/v1/clusters/" + faultPGCluster + "/backups", ID: "b-7", Name: "nightly"}}}
	r := faultAPIRegistry(t, a)
	for _, confirmed := range []bool{false, true} {
		text, isErr := r.handlers["pg_backup_create"](context.Background(), map[string]any{"cluster_id": faultPGCluster, "name": "nightly", "confirmed": confirmed})
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		if !isErr || m["reason"] != "USAGE" || m["existing_id"] != "b-7" {
			t.Errorf("confirmed=%v: want USAGE with existing_id b-7: %s", confirmed, text)
		}
	}
	for _, call := range a.callLog() {
		if strings.HasPrefix(call, http.MethodPost) {
			t.Errorf("a create was sent for a taken name: %s", call)
		}
	}
}

// The previews of dns_record_create, cert_issue_le and cert_import stay
// offline (REL-11): the check of a taken name is the confirmed call's, so
// a call without consent makes no request at all (Qwen review of #239).
func TestNameCheck_OfflinePreviewsMakeNoRequest(t *testing.T) {
	for tool, args := range map[string]map[string]any{
		"dns_record_create": {"zone_id": "z-1", "name": "www", "record_type": "a", "value": "203.0.113.7"},
		"cert_issue_le":     {"name": "evo-le", "dns_names": []any{"www.example.com"}},
		"cert_import":       {"name": "evo-crt", "server_certificate": "Y2VydA==", "private_key": "a2V5"},
	} {
		a := &faultAPI{}
		r := faultAPIRegistry(t, a)
		text, _ := r.handlers[tool](context.Background(), args)
		if !strings.Contains(text, "confirmation_required") {
			t.Errorf("%s: no preview: %s", tool, text)
		}
		if calls := a.callLog(); len(calls) > 0 {
			t.Errorf("%s: the preview reached the cloud: %v", tool, calls)
		}
	}
}

// The world deletes what a DELETE names and lists objects by prefix, as
// the stack tests that run stack_apply and stack_destroy on real handlers
// need (G13-14): a delete that removed nothing answers 404, which is
// «already deleted» since D26 and would pass as silently as a removal.
func TestFaultAPI_DeletesWhatItKeeps(t *testing.T) {
	const sm, vpc = "secretmanager.api.cloud.ru", "vpc.api.cloud.ru"
	routes := "/v1/vpcs/" + faultVPC + "/routes/static"
	a := &faultAPI{made: []faultResource{
		{host: sm, path: "/v2/secret", ID: "s-1", Name: "app/db"},
		{host: vpc, path: "/v1/vpcs", ID: faultVPC, Name: "net"},
		{host: vpc, path: routes, ID: "r-1", Name: "198.51.100.0/24"},
		{host: faultS3Host, path: "bucket", ID: "b", Name: "b"},
		{host: faultS3Host, path: "object b", ID: "logs/a", Name: "logs/a"},
		{host: faultS3Host, path: "object b", ID: "x", Name: "x"},
	}}
	c := &http.Client{Transport: a}
	do := func(method, url string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, url, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	kept := func() []string {
		a.mu.Lock()
		defer a.mu.Unlock()
		var out []string
		for _, r := range a.made {
			out = append(out, r.ID)
		}
		return out
	}

	if code, body := do(http.MethodGet, "https://"+faultS3Host+"/b?list-type=2&prefix=logs/"); code != 200 ||
		!strings.Contains(body, "<Key>logs/a</Key>") || strings.Contains(body, "<Key>x</Key>") {
		t.Errorf("list by prefix logs/: %d %s", code, body)
	}
	for _, d := range []struct{ url, gone string }{
		{"https://" + sm + "/v2/secret/app/db", "s-1"}, // by name, with a folder
		{"https://" + vpc + routes + "/r-1", "r-1"},    // the longest collection, not /v1/vpcs
		{"https://" + faultS3Host + "/b/logs/a", "logs/a"},
	} {
		if code, body := do(http.MethodDelete, d.url); code >= 300 {
			t.Errorf("DELETE %s: %d %s", d.url, code, body)
		}
		if slices.Contains(kept(), d.gone) {
			t.Errorf("DELETE %s left %s: %v", d.url, d.gone, kept())
		}
	}
	if code, _ := do(http.MethodDelete, "https://"+sm+"/v2/secret/app/db"); code != http.StatusNotFound {
		t.Errorf("a second DELETE of the secret: %d, want 404", code)
	}
	if code, _ := do(http.MethodDelete, "https://"+faultS3Host+"/b/logs/a"); code != http.StatusNoContent {
		t.Errorf("a second DELETE of the object: %d, want 204 as S3 answers", code)
	}
	if got := kept(); !slices.Equal(got, []string{faultVPC, "b", "x"}) {
		t.Errorf("kept %v, want the VPC, the bucket and x", got)
	}
}
