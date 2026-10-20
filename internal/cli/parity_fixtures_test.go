// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/stack"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The fake world of TestContract_PreviewParity: the arguments of each tool
// that asks for consent, what some need on disk first, and one fixture per
// API host with the least the previews read (spec.md R2). Every ID is fake
// (00000000-…), every address in 192.0.2.0/24 or 203.0.113.0/24, every
// domain example.com.

// parityArgs are the arguments of every tool that asks for consent (Mutating
// or tools.IsSensitive), as an agent would pass them. A gated tool without
// an entry fails TestContract_PreviewParity; so does an entry that is not
// such a tool.
var parityArgs = map[string]map[string]any{
	// vm, deploy
	"provision":    {"name": "web"},
	"destroy":      {"vm_id": parityVM},
	"deploy":       {},
	"deploy_apply": {"plan_id": "plan-0123456789ab"},
	// image
	"image_create": {"name": "img-web", "disk_id": parityVM},
	"image_delete": {"image_id": parityVM},
	// disk
	"disk_create": {"name": "data", "size_gb": float64(20)},
	"disk_attach": {"disk_id": parityVM, "vm": "web"},
	"disk_detach": {"disk_id": parityVM, "vm": "web"},
	"disk_resize": {"disk_id": parityVM, "size_gb": float64(50)},
	"disk_delete": {"disk_id": parityVM},
	// vpc
	"open_ports":          {"security_group": "web", "ports": []any{float64(80), float64(443)}},
	"fip_attach":          {"fip": parityVM, "vm": "web"},
	"release_floating_ip": {"id": parityVM},
	"vpc_create":          {"name": "net"},
	"vpc_delete":          {"id": parityVM},
	"vpc_route_create":    {"vpc_id": parityVM, "subnet": "192.0.2.0/24", "next_hop_vm_id": parityVM, "next_hop_interface_id": parityNIC},
	"vpc_route_delete":    {"vpc_id": parityVM, "route_id": parityVM},
	"snat_create":         {"name": "nat", "availability_zone_id": pvZone},
	"snat_delete":         {"id": parityVM},
	// k8s
	"k8s_create":       {"name": "k8s-web", "zone": pvZone},
	"k8s_delete":       {"cluster_id": addonClusterID},
	"k8s_add_nodepool": {"cluster_id": addonClusterID, "name": "pool-1", "zone": pvZone},
	"k8s_add_addon":    {"cluster_id": addonClusterID, "name": "cert-manager"},
	// A verb that changes the cluster: a read verb runs without consent.
	"k8s_kubectl": {"cluster_id": addonClusterID, "args": "delete pod web"},
	"k8s_apply":   {"cluster_id": addonClusterID, "manifest": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: web\n"},
	"k8s_helm":    {"cluster_id": addonClusterID, "action": "uninstall", "release": "web"},
	// pg
	"pg_create":        {"name": "web", "specification_id": "std"},
	"pg_delete":        {"cluster_id": parityVM},
	"pg_db_create":     {"cluster_id": parityVM, "name": "app", "owner": "app"},
	"pg_db_delete":     {"cluster_id": parityVM, "name": "app"},
	"pg_user_create":   {"cluster_id": parityVM, "name": "app", "password": "Parity-Pass-0"},
	"pg_user_delete":   {"cluster_id": parityVM, "name": "app"},
	"pg_backup_create": {"cluster_id": parityVM, "name": "nightly"},
	// kafka, redis
	"kafka_create": {"name": "c1", "cpu": float64(2), "ram": float64(4)},
	"kafka_delete": {"cluster_id": parityVM},
	"redis_create": {"name": "c1", "cpu": float64(2), "ram": float64(4)},
	"redis_delete": {"cluster_id": parityVM},
	// lb, tg
	"tg_create": {"name": "tg-web", "vm_id": parityVM},
	"tg_delete": {"id": parityVM},
	"lb_create": {"name": "lb-web", "target_group_id": parityVM, "port": float64(80), "vpc_id": parityVM, "zone": pvZone},
	"lb_delete": {"id": parityVM},
	// dns
	"dns_zone_create":   {"domain": "example.com"},
	"dns_zone_delete":   {"id": parityVM, "type": "public"},
	"dns_record_create": {"zone_id": parityVM, "name": "www", "record_type": "A", "value": "192.0.2.10"},
	"dns_record_delete": {"record_id": parityVM, "type": "public"},
	// secret
	"secret_value":       {"path": "app/db"},
	"secret_create":      {"path": "app/db", "value": "parity-value"},
	"secret_add_version": {"path": "app/db", "value": "parity-value-2"},
	"secret_delete":      {"path": "app/db"},
	// ar, ca
	"ar_create": {"name": "reg"},
	"ar_delete": {"id": parityVM},
	"ca_deploy": {"name": "app", "image": "nginx:1.27"},
	"ca_delete": {"name": "app"},
	"ca_start":  {"name": "app"},
	"ca_stop":   {"name": "app"},
	// cert
	"cert_issue_le":    {"name": "site", "dns_names": []any{"web.example.com"}},
	"cert_import":      {"name": "byo", "server_certificate": "Y2VydA==", "private_key": "a2V5"},
	"cert_renew":       {"certificate_id": parityVM},
	"cert_delete":      {"id": parityVM},
	"cert_private_key": {"certificate_id": parityVM},
	// s3
	"s3_put":           {"bucket": "bucket-web", "key": "k.txt", "content": "hello"},
	"s3_rm":            {"bucket": "bucket-web", "key": "k.txt"},
	"s3_bucket_create": {"bucket": "bucket-web"},
	"s3_bucket_delete": {"bucket": "bucket-web"},
	// stack: the job comes from paritySetup.
	"stack_apply":   {},
	"stack_destroy": {},
}

// paritySetup prepares what a preview reads from disk; it returns the
// arguments it adds and what undoes it.
var paritySetup = map[string]func(t *testing.T, w parityWorld) (map[string]any, func()){
	// A stack planned in this project (under HOME): apply runs its steps;
	// destroy lists what it created (one VPC).
	"stack_apply": func(t *testing.T, _ parityWorld) (map[string]any, func()) {
		return map[string]any{"job_id": parityStackJob(t, "").ID}, func() {}
	},
	"stack_destroy": func(t *testing.T, _ parityWorld) (map[string]any, func()) {
		return map[string]any{"job_id": parityStackJob(t, parityVM).ID}, func() {}
	},
	// deploy in a directory with no deployment: its compose file sizes
	// the VM (as evo deploy does), and the preview prices it.
	"deploy": func(t *testing.T, w parityWorld) (map[string]any, func()) {
		path := filepath.Join(w.dir, "docker-compose.yml")
		writeFile(t, path, "services:\n  web:\n    image: nginx:1.27\n    ports:\n      - \"80:80\"\n")
		return nil, func() { _ = os.Remove(path) }
	},
}

// recordDeployment is deploy's other state: the directory has a running
// deployment, which deploy updates (no new VM, nothing priced).
func recordDeployment(t *testing.T, w parityWorld) (map[string]any, func()) {
	t.Helper()
	compose := filepath.Join(w.dir, "docker-compose.yml")
	writeFile(t, compose, "services:\n  web:\n    image: nginx:1.27\n    ports:\n      - \"80:80\"\n")
	path := state.DefaultPath(w.dir)
	f := &state.File{Deployments: []state.DeploymentRecord{{ID: "dep-parity", VMID: parityVM, VMIP: "203.0.113.10",
		ProjectID: pvProject, Region: pvZone, VMName: "evo-web", Status: "running", ComposeHash: "0000"}}}
	if err := state.Write(path, f); err != nil {
		t.Fatal(err)
	}
	return nil, func() {
		_ = os.Remove(path)
		_ = os.Remove(compose)
	}
}

// parityStackJob saves a planned stack with one VPC; built is the ID of the
// VPC it has created ("" for none).
func parityStackJob(t *testing.T, built string) *stack.Job {
	t.Helper()
	var m stack.Manifest
	if err := json.Unmarshal([]byte(`{"name":"s","vpc":{"name":"v"}}`), &m); err != nil {
		t.Fatal(err)
	}
	j := stack.Plan(m)
	j.ProjectID = pvProject
	j.Steps[0].ResourceID = built
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	return j
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// parityRT answers the APIs the previews read, one fixture per host; any
// other request gets 200 with an empty list. It records every request that
// is not a read: a preview creates nothing.
type parityRT struct {
	addon  *addonAPI
	mu     sync.Mutex
	writes []string
}

// takeWrites returns the writes recorded since the last call.
func (p *parityRT) takeWrites() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.writes
	p.writes = nil
	return w
}

func (p *parityRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		p.mu.Lock()
		p.writes = append(p.writes, r.Method+" "+r.URL.Host+r.URL.Path)
		p.mu.Unlock()
	}
	switch r.URL.Host {
	case "mk8s.api.cloud.ru":
		return p.addon.RoundTrip(r)
	case "postgresql.api.cloud.ru":
		return pgParity(r)
	case "kafka.api.cloud.ru", "redis.api.cloud.ru":
		return dbaasParity(r)
	case "compute.api.cloud.ru":
		if w := computeParity(r); w != nil {
			return w.Result(), nil
		}
	}
	return contractRT{status: 200, body: `{"items":[],"total":0}`}.RoundTrip(r)
}

// parityNIC is the network interface of the VM parityVM.
const parityNIC = "00000000-0000-4000-8000-0000000000f1"

// computeParity: the compute API beyond previewCloud — the VM with its
// interface (tg_create picks it), the project's SNAT gateway
// (snat_delete names it); nil for the rest.
func computeParity(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	switch r.URL.Path {
	case "/api/v1/vms/" + parityVM:
		_, _ = io.WriteString(w, `{"id":"`+parityVM+`","name":"web","state":"running","interfaces":[{"id":"`+parityNIC+`","type":"regular","ip_address":"192.0.2.5"}]}`)
	case "/api/v1/nat-gateways":
		_, _ = io.WriteString(w, `{"items":[{"id":"`+parityVM+`","name":"nat","status":"ACTIVE"}],"total":1}`)
	default:
		return nil
	}
	return w
}

// dbaasParity: the versions and the specifications a create picks from
// (Managed Kafka and Redis answer alike).
func dbaasParity(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	switch r.URL.Path {
	case "/v1/clusters":
		_, _ = io.WriteString(w, `{"clusters":[]}`)
	case "/v1/clusters/" + parityVM:
		_, _ = io.WriteString(w, `{"id":"`+parityVM+`","name":"c1","status":"RUNNING"}`)
	case "/v1/versions":
		_, _ = io.WriteString(w, `{"versions":[{"id":"v1","name":"1"}]}`)
	case "/v1/specifications":
		_, _ = io.WriteString(w, `{"specifications":[{"id":"s-1","name":"small","cpu":2,"memory":4}]}`)
	default:
		http.Error(w, `{"message":"no such path"}`, 404)
	}
	return w.Result(), nil
}

func pgParity(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	switch {
	case r.URL.Path == "/v1/clusters":
		_, _ = io.WriteString(w, `{"clusters":[]}`)
	case r.URL.Path == "/v1/clusters/"+parityVM:
		_, _ = io.WriteString(w, `{"id":"`+parityVM+`","name":"web","status":"RUNNING"}`)
	case r.URL.Path == "/v1/versions":
		_, _ = io.WriteString(w, `{"versions":[{"id":"v17","name":"17"}]}`)
	case r.URL.Path == "/v1/specifications":
		_, _ = io.WriteString(w, `{"specifications":[{"id":"std","display_name":"std","cpu":2,"memory":4,"deployment_mode":"STANDARD","min_storage_gb":20,"max_hosts":2}]}`)
	default:
		http.Error(w, `{"message":"no such path"}`, 404)
	}
	return w.Result(), nil
}
