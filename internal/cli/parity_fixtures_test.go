// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/stack"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The fake world of the parity tests (TestContract_PreviewParity,
// TestParity_JSONEqualsTool, TestParity_NewCommandsJSONEqualsToolsCall):
// the arguments of each tool, what some need on disk first, and one fixture
// per API host with the least the previews read (spec.md R2). Every ID is
// fake (00000000-…), every address in 192.0.2.0/24 or 203.0.113.0/24 —
// but a new subnet's network, which the API takes only private
// (10.10.0.0/24) — every domain example.com.

// parityArgs are the arguments of every tool, as an agent would pass them:
// one table for the previews of the tools that ask for consent (Mutating or
// tools.IsSensitive, REQ-6) and for the answers of every tool with a command
// (REQ-8). A tool without an entry fails TestParity_JSONEqualsTool (and,
// when it asks for consent, TestContract_PreviewParity); so does an entry
// that is no tool. What a tool needs on disk first comes from paritySetup,
// which may replace an argument here (stack_apply: the job it plans).
var parityArgs = map[string]map[string]any{
	// vm, deploy
	"provision":    {"name": "web"},
	"destroy":      {"vm_id": parityVM},
	"deploy":       {},
	"deploy_apply": {"plan_id": "plan-0123456789ab"},
	"deploy_plan":  {},
	"status":       {},
	"list":         {},
	"logs":         {"tail": float64(100)},
	"vm_get":       {"vm_id": parityVM},
	"vm_list":      {},
	"list_zones":   {},
	"list_flavors": {},
	"list_images":  {},
	// image, marketplace
	"image_list":       {"query": "ubuntu"},
	"image_get":        {"image_id": pvImage},
	"marketplace_list": {},
	"image_create":     {"name": "img-web", "disk_id": parityVM},
	"image_delete":     {"image_id": parityVM},
	// disk
	"disk_create":     {"name": "data", "size_gb": float64(20)},
	"disk_attach":     {"disk_id": parityVM, "vm": "web"},
	"disk_detach":     {"disk_id": parityVM, "vm": "web"},
	"disk_resize":     {"disk_id": parityVM, "size_gb": float64(50)},
	"disk_delete":     {"disk_id": parityVM},
	"disk_list":       {},
	"list_disk_types": {},
	// vpc
	// The zone's group, in the worlds of the previews and of the answers
	// alike: open_ports reads it before the question (G13-23).
	"open_ports":           {"security_group": "SSH-access_ru.AZ-1", "ports": []any{float64(80), float64(443)}},
	"close_ports":          {"security_group": "SSH-access_ru.AZ-1", "ports": []any{float64(80)}},
	"vpc_list":             {},
	"vpc_get":              {"id": parityVM},
	"vpc_operation":        {"operation_id": parityVM},
	"vpc_routes":           {"vpc_id": parityVM},
	"list_subnets":         {},
	"list_security_groups": {},
	"list_floating_ips":    {},
	"snat_list":            {},
	"fip_attach":           {"fip": parityVM, "vm": "web"},
	"release_floating_ip":  {"id": parityVM},
	"vpc_create":           {"name": "net"},
	"vpc_delete":           {"id": parityVM},
	"vpc_route_create":     {"vpc_id": parityVM, "subnet": "192.0.2.0/24", "next_hop_vm_id": parityVM, "next_hop_interface_id": parityNIC},
	"vpc_route_delete":     {"vpc_id": parityVM, "route_id": parityVM},
	"snat_create":          {"name": "nat-new", "availability_zone_id": pvZone},
	"snat_delete":          {"id": parityVM},
	"subnet_create":        {"name": "sub-new", "cidr": "10.10.0.0/24", "zone": "ru.AZ-1", "dns_servers": []any{"192.0.2.53"}},
	"subnet_delete":        {"subnet_id": parityVM},
	// k8s
	"k8s_create":         {"name": "k8s-web", "zone": pvZone, "cluster_sa_id": parityVM},
	"k8s_plan":           {"name": "k8s-web", "zone": pvZone, "cluster_sa_id": parityVM},
	"k8s_list":           {},
	"k8s_get":            {"cluster_id": addonClusterID},
	"k8s_options":        {},
	"k8s_kubeconfig":     {"cluster_id": addonClusterID},
	"k8s_nodepools":      {"cluster_id": addonClusterID},
	"k8s_addons":         {"query": "cert"},
	"k8s_addon_versions": {"name": "cert-manager"},
	"k8s_delete":         {"cluster_id": addonClusterID},
	"k8s_add_nodepool":   {"cluster_id": addonClusterID, "name": "pool-1", "zone": pvZone},
	"k8s_add_addon":      {"cluster_id": addonClusterID, "name": "cert-manager"},
	// A verb that changes the cluster: a read verb runs without consent.
	"k8s_kubectl": {"cluster_id": addonClusterID, "args": "delete pod web"},
	"k8s_apply":   {"cluster_id": addonClusterID, "manifest": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: web\n"},
	"k8s_helm":    {"cluster_id": addonClusterID, "action": "uninstall", "release": "web"},
	// pg
	"pg_create":        {"name": "web", "specification_id": "std"},
	"pg_delete":        {"cluster_id": parityVM},
	"pg_db_create":     {"cluster_id": parityVM, "name": "app", "owner": "app"},
	"pg_db_delete":     {"cluster_id": parityVM, "name": "old"},
	"pg_user_create":   {"cluster_id": parityVM, "name": "app", "password": "Parity-Pass-0"},
	"pg_user_delete":   {"cluster_id": parityVM, "name": "old"},
	"pg_backup_create": {"cluster_id": parityVM, "name": "nightly"},
	"pg_list":          {},
	"pg_get":           {"cluster_id": parityVM},
	"pg_versions":      {},
	"pg_specs":         {},
	"pg_operation":     {"operation_id": parityVM},
	"pg_databases":     {"cluster_id": parityVM},
	"pg_users":         {"cluster_id": parityVM},
	"pg_backups":       {"cluster_id": parityVM},
	// kafka, redis
	"kafka_create":    {"name": "c1", "cpu": float64(2), "ram": float64(4)},
	"kafka_delete":    {"cluster_id": parityVM},
	"redis_create":    {"name": "c1", "cpu": float64(2), "ram": float64(4)},
	"redis_delete":    {"cluster_id": parityVM},
	"kafka_list":      {},
	"kafka_get":       {"cluster_id": parityVM},
	"kafka_versions":  {},
	"kafka_specs":     {},
	"kafka_operation": {"operation_id": parityVM},
	"redis_list":      {},
	"redis_get":       {"cluster_id": parityVM},
	"redis_versions":  {},
	"redis_specs":     {},
	"redis_operation": {"operation_id": parityVM},
	// lb, tg
	"tg_create":    {"name": "tg-web", "vm_id": parityVM},
	"tg_delete":    {"id": parityVM},
	"lb_create":    {"name": "lb-web", "target_group_id": parityVM, "port": float64(80), "vpc_id": parityVM, "zone": pvZone},
	"lb_delete":    {"id": parityVM},
	"lb_list":      {},
	"lb_get":       {"id": parityVM},
	"lb_operation": {"operation_id": parityVM},
	"tg_list":      {},
	"tg_get":       {"id": parityVM},
	// dns
	"dns_zone_create":   {"domain": "example.com"},
	"dns_zone_delete":   {"id": parityVM, "type": "public"},
	"dns_record_create": {"zone_id": parityVM, "name": "www", "record_type": "A", "value": "192.0.2.10"},
	"dns_record_delete": {"record_id": parityVM, "type": "public"},
	"dns_zones":         {},
	"dns_zone_get":      {"id": parityVM},
	"dns_operation":     {"operation_id": parityVM},
	"dns_records":       {"zone_id": parityVM},
	"dns_record_get":    {"record_id": parityVM},
	// secret
	"secret_value":       {"path": "app/db"},
	"secret_create":      {"path": "app/db", "value": "parity-value"},
	"secret_add_version": {"path": "app/db", "value": "parity-value-2"},
	"secret_delete":      {"path": "app/db"},
	"secret_list":        {},
	"secret_get":         {"path": "app/db"},
	// ar, ca
	"ar_create":    {"name": "reg"},
	"ar_delete":    {"id": parityVM},
	"ca_deploy":    {"name": "app", "image": "nginx:1.27"},
	"ca_delete":    {"name": "app"},
	"ca_start":     {"name": "app"},
	"ca_stop":      {"name": "app"},
	"ar_list":      {},
	"ar_get":       {"id": parityVM},
	"ar_operation": {"operation_id": parityVM},
	"ca_list":      {},
	"ca_get":       {"name": "app"},
	"ca_logs":      {"name": "app"},
	// cert
	"cert_issue_le":    {"name": "site", "dns_names": []any{"web.example.com"}},
	"cert_import":      {"name": "byo", "server_certificate": "Y2VydA==", "private_key": "a2V5"},
	"cert_renew":       {"certificate_id": parityVM},
	"cert_delete":      {"id": parityVM},
	"cert_private_key": {"certificate_id": parityVM},
	"cert_list":        {},
	"cert_get":         {"id": parityVM},
	"cert_usage":       {"id": parityVM},
	"cert_versions":    {"certificate_id": parityVM},
	"cert_operation":   {"operation_id": parityVM},
	// s3
	"s3_put":           {"bucket": "bucket-web", "key": "k.txt", "content": "hello"},
	"s3_rm":            {"bucket": "bucket-web", "key": "k.txt"},
	"s3_bucket_create": {"bucket": "bucket-web"},
	"s3_bucket_delete": {"bucket": "bucket-empty"},
	"s3_buckets":       {},
	"s3_ls":            {"bucket": "bucket-web"},
	"s3_get":           {"bucket": "bucket-web", "key": "k.txt"},
	// stack: the job of apply, destroy and status comes from paritySetup;
	// the id here is what a command line without one gets.
	"stack_plan":    {"manifest": map[string]any{"name": "s", "vpc": map[string]any{"name": "v"}}},
	"stack_apply":   {"job_id": "stk-0123456789"},
	"stack_destroy": {"job_id": "stk-0123456789"},
	"stack_status":  {"job_id": "stk-0123456789"},
	// billing, audit, org, iam, metrics. project_id of these is a filter
	// of what is read, given with --project-id of the command.
	"billing_balance":      {},
	"billing_consumption":  {"project_id": pvProject, "start": "2026-07-01"},
	"billing_finops":       {"project_id": pvProject},
	"audit_events":         {},
	"org_projects":         {},
	"org_quotas":           {"project_id": pvProject},
	"iam_service_accounts": {"project_id": pvProject},
	"metrics_history":      {"match": "up"},
	"metrics_scrape":       {},
	// auth
	"whoami": {},
	"doctor": {},
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
	"stack_status": func(t *testing.T, _ parityWorld) (map[string]any, func()) {
		return map[string]any{"job_id": parityStackJob(t, "").ID}, func() {}
	},
	// status and logs of the deployment of the directory.
	"status": recordDeployment,
	"logs":   recordDeployment,
	// A plan of deploy_plan (no compose file: a bare VM), which apply
	// executes; a plan the fake cannot make leaves the id of parityArgs.
	"deploy_apply": func(t *testing.T, _ parityWorld) (map[string]any, func()) {
		plan, _ := newRegistry().Lookup("deploy_plan")
		text, isErr := plan.Handler(context.Background(), map[string]any{})
		id, _ := parseObject(text)["plan_id"].(string)
		if isErr || id == "" {
			return nil, func() {}
		}
		return map[string]any{"plan_id": id}, func() {}
	},
	// fip_attach: a free floating IP (the preview world has none, so that
	// destroy bills none); it looks the address up before the question
	// (W6).
	"fip_attach": func(t *testing.T, _ parityWorld) (map[string]any, func()) {
		world := toolEngine
		toolEngine = func(o map[string]any) (*engine.Engine, error) {
			e, err := world(o)
			if err == nil {
				e.Cloud = freeFIPCloud{e.Cloud}
			}
			return e, err
		}
		return nil, func() { toolEngine = world }
	},
	// close_ports: the zone's group (in the worlds of the previews and of
	// the answers alike) holds the rule of port 80 that open_ports adds —
	// the world's groups have no rules, as open_ports needs. The world as
	// it was answers first, a failing cloud included
	// (TestParity_ErrorReasonEqualsTool).
	"close_ports": func(t *testing.T, w parityWorld) (map[string]any, func()) {
		world := w.rt.extra
		w.rt.extra = func(r *http.Request) (*http.Response, error) {
			if world != nil {
				if resp, err := world(r); resp != nil || err != nil {
					return resp, err
				}
			}
			if r.URL.Host != "compute.api.cloud.ru" || !strings.HasPrefix(r.URL.Path, "/api/v1/security-groups/") || !strings.Contains(r.URL.Path, "/rules") {
				return nil, nil
			}
			if r.Method == http.MethodDelete {
				return contractRT{status: http.StatusNoContent}.RoundTrip(r)
			}
			return contractRT{status: 200, body: `{"items":[{"id":"` + parityNIC + `","direction":"ingress","ip_protocol":"tcp",` +
				`"port_range":"80:80","remote_ip_prefix":"0.0.0.0/0"}],"total":1}`}.RoundTrip(r)
		}
		return nil, func() { w.rt.extra = world }
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
		ProjectID: pvProject, Region: pvZone, VMName: "evo-web", Status: "running", ComposeHash: "0000", SSHUser: "user1"}}}
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
	addon *addonAPI
	// extra answers first when set (TestParity_JSONEqualsTool: the APIs
	// the answers read beyond the previews; TestParity_ErrorReasonEqualsTool:
	// a failing cloud); nil and no error pass the request on.
	extra  func(r *http.Request) (*http.Response, error)
	mu     sync.Mutex
	writes []string
	// unknown are the requests no fixture answers (parityAnswers,
	// parityEmptyLists): withParityWorld fails the test on them.
	unknown map[string]bool
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
	if p.extra != nil {
		if resp, err := p.extra(r); resp != nil || err != nil {
			return resp, err
		}
	}
	switch r.URL.Host {
	case "mk8s.api.cloud.ru":
		if r.URL.Path == "/v3/productConfiguration" {
			// The catalogue the preview of k8s_create resolves its plan
			// from, and k8s_add_nodepool its flavor (D29).
			return contractRT{status: 200, body: k8sProductConfiguration}.RoundTrip(r)
		}
		return p.noPath(r)(p.addon.RoundTrip(r))
	case "postgresql.api.cloud.ru":
		return p.noPath(r)(pgParity(r))
	case "kafka.api.cloud.ru", "redis.api.cloud.ru":
		return p.noPath(r)(dbaasParity(r))
	case "compute.api.cloud.ru":
		if w := computeParity(r); w != nil {
			return w.Result(), nil
		}
	case "s3.cloud.ru":
		return s3Parity(r), nil
	case "vpc.api.cloud.ru":
		// The route vpc_route_delete removes: its preview looks it up (D26).
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/routes/static") {
			return contractRT{status: 200, body: `{"routes":[{"id":"` + parityVM + `","subnet":"198.51.100.0/24"}]}`}.RoundTrip(r)
		}
	}
	key := parityRequest(r)
	if body, ok := parityAnswers[key]; ok {
		return contractRT{status: 200, body: body}.RoundTrip(r)
	}
	if _, ok := parityEmptyLists[key]; !ok {
		p.markUnknown(key)
	}
	return contractRT{status: 200, body: `{"items":[],"total":0}`}.RoundTrip(r)
}

// k8sProductConfiguration is the catalogue of Managed Kubernetes of the
// parity worlds: one zone, one flavor, one version. A zone names its
// flavors by name, as the platform does (K8S-16, platform-answers.json).
const k8sProductConfiguration = `{"defaultZone":"` + pvZone + `","defaultChannel":"stable","defaultDiskType":"SSD",` +
	`"zones":[{"zoneId":"` + pvZone + `","name":"ru.AZ-1","availableFlavors":["gen-2-4"]}],` +
	`"flavors":[{"flavorId":"` + pvFlavor + `","name":"gen-2-4","cpu":2,"ram":4}],` +
	`"channels":[{"channel":"stable","availableVersions":["v1.31.4"]}]}`

func (p *parityRT) markUnknown(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.unknown == nil {
		p.unknown = map[string]bool{}
	}
	p.unknown[key] = true
}

// noPath names the requests the fakes of one API (mk8s, PostgreSQL, Kafka
// and Redis) answer 404 for want of a path: since D26 a 404 on a delete is
// «already deleted», code 0, and would pass as silently as an empty list.
func (p *parityRT) noPath(r *http.Request) func(*http.Response, error) (*http.Response, error) {
	return func(resp *http.Response, err error) (*http.Response, error) {
		if err == nil && resp.StatusCode == http.StatusNotFound {
			p.markUnknown(parityRequest(r))
		}
		return resp, err
	}
}

// takeUnknown returns the requests no fixture answers, sorted.
func (p *parityRT) takeUnknown() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for k := range p.unknown {
		out = append(out, k)
	}
	p.unknown = nil
	sort.Strings(out)
	return out
}

var uuidInPath = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// parityRequest names a request as the fixtures do: the method, the host and
// the path with every ID as {id}.
func parityRequest(r *http.Request) string {
	return r.Method + " " + r.URL.Host + uuidInPath.ReplaceAllString(r.URL.Path, "{id}")
}

// parityObject is one resource of the world: the fields the tools read
// first, in the shape most Cloud.ru APIs use.
const parityObject = `{"id":"` + parityVM + `","name":"web","status":"ACTIVE","state":"ACTIVE"}`

// parityAnswers answer the reads of one resource and the writes that the
// fixtures above do not model: an empty list there would be no resource at
// all, and a tool that misreads it would still pass parity (delta audit
// 1.2.3). A request neither here, in the fixtures nor in parityEmptyLists
// fails the test that made it (withParityWorld).
var parityAnswers = map[string]string{
	// Reachability probes of doctor and whoami.
	"GET compute.api.cloud.ru/": `{}`,
	"GET iam.api.cloud.ru/":     `{}`,
	// One resource.
	"GET billing.api.cloud.ru/v1/agreement/{id}":                                    `{"balance":1000,"active":true}`,
	"GET ar.api.cloud.ru/v1/registries/{id}":                                        parityObject,
	"GET certificatemanager.api.cloud.ru/v1/certificate/{id}":                       parityObject,
	"GET certificatemanager.api.cloud.ru/v1/certificate/{id}:usage":                 `{"usage":[]}`,
	"GET certificatemanager.api.cloud.ru/v1/certificate/{id}/version/0:private_key": `{"private_key":"-----BEGIN PRIVATE KEY-----\nparity\n-----END PRIVATE KEY-----\n"}`,
	"GET certificatemanager.api.cloud.ru/v1/operation/{id}":                         jsonOp,
	"GET compute.api.cloud.ru/api/v1/disks/{id}":                                    `{"id":"` + parityVM + `","name":"old","size":20,"state":"available"}`,
	"GET compute.api.cloud.ru/api/v1/floating-ips/{id}":                             `{"id":"` + parityVM + `","ip_address":"203.0.113.10","state":"available"}`,
	"GET containers.api.cloud.ru/v2/containers/app":                                 `{"name":"app","status":"RUNNING"}`,
	"GET dns.api.cloud.ru/v1/publicRecordsSole/{id}":                                `{"id":"` + parityVM + `","name":"www","type":"A","values":["192.0.2.5"]}`,
	"GET dns.api.cloud.ru/v1/publicZones/{id}":                                      `{"id":"` + parityVM + `","name":"example","domain":"example.com.","state":"ok"}`,
	// The zone dns_record_create adds to: it looks the zone up before the
	// question (W6).
	"GET dns.api.cloud.ru/v1/publicZones":               `{"zones":[{"id":"` + parityVM + `","name":"example","domain":"example.com.","state":"ok"}]}`,
	"GET nlb.api.cloud.ru/v2/networkLoadBalancers/{id}": parityObject,
	"GET nlb.api.cloud.ru/v2/targetGroups/{id}":         parityObject,
	"GET secretmanager.api.cloud.ru/v2/secret/app/db":   `{"secret":{"id":"` + parityVM + `","path":"app/db"}}`,
	"GET vpc.api.cloud.ru/v1/vpcs/{id}":                 parityObject,
	// The project's default VPC: subnet_create puts its subnet there.
	"GET vpc.api.cloud.ru/v1/vpcs":                    `{"vpcs":[{"id":"` + parityVM + `","name":"default","default":true}],"total":1}`,
	"GET compute.api.cloud.ru/api/v1/subnets/{id}":    `{"id":"` + parityVM + `","name":"sub","state":"created","can_delete":true}`,
	"DELETE compute.api.cloud.ru/api/v1/subnets/{id}": `{}`,
	// Writes: the operation or the empty body the API answers with.
	"DELETE certificatemanager.api.cloud.ru/v1/certificate/{id}":     jsonOp,
	"POST certificatemanager.api.cloud.ru/v1/certificate/{id}:renew": jsonOp,
	"DELETE compute.api.cloud.ru/api/v1/disks/{id}":                  `{}`,
	"PUT compute.api.cloud.ru/api/v1/disks/{id}":                     `{}`,
	"POST compute.api.cloud.ru/api/v1/disks/{id}/attach":             `{}`,
	"POST compute.api.cloud.ru/api/v1/disks/{id}/detach":             `{}`,
	"DELETE compute.api.cloud.ru/api/v1/floating-ips/{id}":           `{}`,
	"DELETE compute.api.cloud.ru/api/v1/images/{id}":                 `{}`,
	"DELETE compute.api.cloud.ru/api/v1/nat-gateways/{id}":           `{}`,
	"DELETE secretmanager.api.cloud.ru/v2/secret/app/db":             `{}`,
	"POST secretmanager.api.cloud.ru/v2/version":                     `{"version":{"id":"` + parityVM + `","secret_id":"` + parityVM + `"}}`,
}

// parityEmptyLists are the lists the world leaves empty on purpose: the
// answer {"items":[],"total":0} is the model, and the reason says why
// nothing in it is enough for the tool. Only lists (a GET whose last segment
// is no {id} and no :action, TestParityWorld_EmptyListsAreLists).
var parityEmptyLists = map[string]string{
	"GET ar.api.cloud.ru/v1/registries":                                "the project has no registry yet: ar_create's name is free",
	"GET audit.api.cloud.ru/events/list/{id}":                          "no audit events: the answer is the empty page on both surfaces",
	"GET certificatemanager.api.cloud.ru/v1/certificate":               "no certificate yet: cert_import and cert_issue_le names are free",
	"GET certificatemanager.api.cloud.ru/v1/certificate/{id}/versions": "no versions: cert_get lists none",
	"GET compute.api.cloud.ru/api/v1/images":                           "no private image: image_create's name is free, image_list empty",
	"GET compute.api.cloud.ru/api/v1/security-groups/{id}/rules":       "a group without rules: open_ports adds them all",
	"GET containers.api.cloud.ru/v2/containers":                        "no container app yet: ca_deploy's name is free",
	"GET containers.api.cloud.ru/v2/containers/app/logs":               "no log lines",
	"GET dns.api.cloud.ru/v1/privateZones":                             "no private zone: the zone of dns_record_create is public",
	"GET dns.api.cloud.ru/v1/publicRecordsSole":                        "the zone has no records yet: dns_record_create's name is free",
	"GET iam.api.cloud.ru/api/v2/serviceAccounts":                      "no service account: sa_list empty",
	"GET monitoring.api.cloud.ru/v1/{id}/export_metrics":               "no metrics",
	"GET monitoring.api.cloud.ru/v2/{id}/scrape_metrics":               "no metrics",
	"GET nlb.api.cloud.ru/v2/networkLoadBalancers":                     "no balancer yet: lb_create's name is free",
	"GET nlb.api.cloud.ru/v2/targetGroups":                             "no target group yet: tg_create's name is free",
	"GET organization.api.cloud.ru/v2/consumption":                     "no consumption in the period",
	"GET organization.api.cloud.ru/v3/projects":                        "no other project: org_projects empty",
	"GET organization.api.cloud.ru/v3/quotas/usages":                   "no quota usage rows",
	"GET organization.api.cloud.ru/v3/quotas/resourceTypes":            "no quota types: rows keep their codes",
	"GET secretmanager.api.cloud.ru/v2/secret":                         "no secret yet: secret_create's path is free",
	"GET secretmanager.api.cloud.ru/v2/version/app/db":                 "no version of the secret: secret_value finds none",
}

// parityNIC is the network interface of the VM parityVM.
const parityNIC = "00000000-0000-4000-8000-0000000000f1"

// computeParity: the compute API beyond previewCloud — the VM with its
// interface (tg_create picks it), the project's disk and SNAT gateway
// (disk_delete and snat_delete look them up; image_create takes the zone
// of the disk, which the API always names); nil for the rest.
func computeParity(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	switch r.URL.Path {
	case "/api/v1/vms/" + parityVM:
		_, _ = io.WriteString(w, `{"id":"`+parityVM+`","name":"web","state":"running","interfaces":[{"id":"`+parityNIC+`","name":"eth0","type":"regular","ip_address":"192.0.2.5"}]}`)
	case "/api/v1/disks":
		if r.Method != http.MethodPost {
			_, _ = io.WriteString(w, `{"items":[{"id":"`+parityVM+`","name":"old","size":20,"availability_zone":{"id":"`+pvZone+`"}}],"total":1}`)
			break
		}
		_, _ = io.WriteString(w, `{"id":"`+parityVM+`","name":"data","size":20,"status":"creating"}`)
	case "/api/v1/subnets":
		// The list is previewCloud's; the create answers the new subnet.
		_, _ = io.WriteString(w, `{"id":"`+parityVM+`","name":"sub-new","state":"creating"}`)
	case "/api/v1/nat-gateways":
		if r.Method == http.MethodPost {
			_, _ = io.WriteString(w, `{"id":"`+parityVM+`","name":"nat-new","status":"CREATING"}`)
			break
		}
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
	case strings.HasSuffix(r.URL.Path, "/databases") && r.Method == http.MethodGet:
		_, _ = io.WriteString(w, `{"databases":[{"name":"old"}]}`) // pg_db_delete looks it up (D26)
	case strings.HasSuffix(r.URL.Path, "/users") && r.Method == http.MethodGet:
		_, _ = io.WriteString(w, `{"users":[{"name":"old"}]}`) // pg_user_delete looks it up (D26)
	case strings.HasSuffix(r.URL.Path, "/backups") && r.Method == http.MethodGet:
		_, _ = io.WriteString(w, `{"backups":[]}`) // pg_backup_create checks the name is free
	default:
		http.Error(w, `{"message":"no such path"}`, 404)
	}
	return w.Result(), nil
}

// freeFIPCloud has the free floating IP fip_attach attaches.
type freeFIPCloud struct{ provision.CloudAPI }

func (freeFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: parityVM, IPAddress: "203.0.113.10", State: "available"}}, nil
}
