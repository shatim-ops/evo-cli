// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// Находки внешнего релизного аудита по k8s, NLB, стекам и Container Apps
// (L-08, L-10, L-26…L-29, L-36, L-37, A-024). Все UUID выдуманы; ответы
// API — по спецификациям mk8s v3, nlb v2, compute v3.
const (
	raProject = "00000000-0000-0000-0000-0000000000e1"
	raAZ1     = "00000000-0000-0000-0000-00000000aa01"
	raAZ2     = "00000000-0000-0000-0000-00000000aa02"
	raImage   = "00000000-0000-0000-0000-00000000b001"
)

// raCloud — compute API: две зоны, подсети по зонам и VPC, один образ.
type raCloud struct {
	provision.CloudAPI
	subnets []*provision.Subnet
	sgs     []*provision.SecurityGroup
}

func (c raCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return c.sgs, nil
}

func (raCloud) SetToken(string) {}
func (raCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: raAZ1, Name: "ru.AZ-1"}, {ID: raAZ2, Name: "ru.AZ-2"}}, nil
}
func (c raCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return c.subnets, nil
}
func (raCloud) ListImages(_ context.Context, f ...provision.ImageFilter) ([]*provision.Image, error) {
	if len(f) > 0 && f[0].Name == "" && f[0].DisplayName != "" {
		return nil, nil
	}
	return []*provision.Image{{ID: raImage, Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Type: "public",
		AvailZones: []provision.ImageAvailZone{{AvailabilityZoneID: raAZ1, State: "created"}, {AvailabilityZoneID: raAZ2, State: "created"}}}}, nil
}
func (raCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, nil
}
func (raCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	if id != raImage {
		return nil, nil
	}
	return &provision.ImageDetail{ID: raImage, Name: "Ubuntu-24.04", Type: "public"}, nil
}

// raSubnets: по подсети в каждой зоне в vpc-1, ещё одна в ru.AZ-1 в
// чужой VPC — первой в списке, как subs[0] у старого кода.
func raSubnets() []*provision.Subnet {
	return []*provision.Subnet{
		{ID: "sn-other-vpc", Name: "Other_ru.AZ-1", AvailabilityZoneID: raAZ1, VPCID: "vpc-other", Default: true},
		{ID: "sn-2", Name: "Default_ru.AZ-2", AvailabilityZoneID: raAZ2, VPCID: "vpc-1", Default: true},
		{ID: "sn-1", Name: "Default_ru.AZ-1", AvailabilityZoneID: raAZ1, VPCID: "vpc-1", Default: true},
	}
}

// raAPI — mk8s, nlb и Container Apps за http.DefaultTransport.
type raAPI struct {
	mu        sync.Mutex
	reqs      []string
	posts     map[string]string // путь → тело последнего POST
	targetVMs string            // target_vms целевой группы tg-1
	flavors   string            // flavors of productConfiguration; empty — gen-2-4 only
	pools     string            // nodePools of the cluster
}

func (f *raAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.Host+r.URL.Path)
	if r.Method == http.MethodPost {
		b, _ := io.ReadAll(r.Body)
		if f.posts == nil {
			f.posts = map[string]string{}
		}
		f.posts[r.URL.Path] = string(b)
	}
	switch {
	case r.URL.Path == "/v3/productConfiguration":
		flavors := f.flavors
		if flavors == "" {
			flavors = `[{"flavorId":"fl-1","name":"gen-2-4","cpu":2,"ram":4}]`
		}
		_, _ = io.WriteString(rec, `{"defaultZone":"ru.AZ-2",
			"zones":[{"zoneId":"mk-az1","name":"ru.AZ-1"},{"zoneId":"mk-az2","name":"ru.AZ-2"}],
			"flavors":`+flavors+`,
			"channels":[{"channel":"STABLE","availableVersions":["v1.34.6"]}]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/clusters":
		_, _ = io.WriteString(rec, `{"clusters":[]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v2/networkLoadBalancers":
		_, _ = io.WriteString(rec, `{"balancers":[]}`)
	case r.Method == http.MethodPost && r.URL.Path == "/v3/clusters":
		_, _ = io.WriteString(rec, `{"id":"op-1","resourceId":"cl-1","done":false}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/clusters/cl-1":
		_, _ = io.WriteString(rec, `{"id":"cl-1","controlPlane":{"zones":["mk-az1"]}}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/clusters/cl-nozone":
		_, _ = io.WriteString(rec, `{"id":"cl-nozone","controlPlane":{"zones":[]}}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/nodePools":
		_, _ = io.WriteString(rec, `{"nodePools":[`+f.pools+`]}`)
	case r.Method == http.MethodPost && r.URL.Path == "/v3/nodePools":
		_, _ = io.WriteString(rec, `{"id":"op-np","resourceId":"np-1","done":false}`)
	case r.URL.Path == "/v2/targetGroups/tg-1":
		_, _ = io.WriteString(rec, `{"id":"tg-1","name":"tg","vpc_id":"vpc-1","target_vms":[`+f.targetVMs+`]}`)
	case r.Method == http.MethodPost && r.URL.Path == "/v2/networkLoadBalancers":
		_, _ = io.WriteString(rec, `{"id":"op-lb","done":false}`)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers"):
		_, _ = io.WriteString(rec, `{"id":"op-ca","done":false}`)
	default:
		http.Error(rec, `{"message":"not found"}`, 404)
	}
	return rec.Result(), nil
}

func (f *raAPI) post(path string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var m map[string]any
	if b, ok := f.posts[path]; ok {
		_ = json.Unmarshal([]byte(b), &m)
	}
	return m
}

func (f *raAPI) postCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, q := range f.reqs {
		if strings.HasPrefix(q, "POST ") {
			n++
		}
	}
	return n
}

// raRegistry — реестр с ключом и проектом; cfg дополняет конфигурацию
// (регион, образ по умолчанию).
// raRegistry takes any fake API: the fuzzers answer with their own bytes.
func raRegistry(tb testing.TB, api http.RoundTripper, cloud provision.CloudAPI, cfg func(*config.Config)) *Registry {
	tb.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = api
	tb.Cleanup(func() { http.DefaultTransport = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		c := config.Config{ProjectID: raProject, KeyID: "k", Secret: "s"}
		if cfg != nil {
			cfg(&c)
		}
		return &engine.Engine{
			Config:  c,
			Auth:    staticToken{},
			Cloud:   cloud,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

func wantReason(t *testing.T, label string, m map[string]any, text, reason string) {
	t.Helper()
	if m["reason"] != reason {
		t.Errorf("%s: reason %v, want %s: %s", label, m["reason"], reason, text)
	}
}

// L-08: зона мастеров не уходила в запрос — мастер вставал в ru.AZ-2, а
// VIP-подсеть бралась из ru.AZ-1 (или первая в проекте). Теперь zones —
// идентификатор зоны mk8s, подсеть VIP — из той же зоны, чужая — отказ.
func TestK8s_ControlPlaneZoneAndVIPSubnet(t *testing.T) {
	f := &raAPI{}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)

	m, text, isErr := callNamed(t, r, "k8s_create", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1", "confirmed": true})
	if isErr {
		t.Fatalf("k8s_create: %s", text)
	}
	body := f.post("/v3/clusters")
	cp, _ := body["controlPlaneConfiguration"].(map[string]any)
	if zones, _ := cp["zones"].([]any); len(zones) != 1 || zones[0] != "mk-az1" {
		t.Errorf("controlPlaneConfiguration.zones = %v, want [mk-az1] (%v)", cp["zones"], m)
	}
	nc, _ := body["networkConfiguration"].(map[string]any)
	if nc["privateVipSubnetId"] != "sn-1" && nc["privateVipSubnetId"] != "sn-other-vpc" {
		t.Errorf("VIP subnet %v is not of ru.AZ-1", nc["privateVipSubnetId"])
	}

	// Зона по подсети, когда зона не задана.
	m, text, isErr = callNamed(t, r, "k8s_plan", map[string]any{"subnet_id": "sn-1", "cluster_sa_id": "sa-1"})
	if isErr || m["zone"] != "ru.AZ-1" || m["subnet_id"] != "sn-1" {
		t.Errorf("plan by subnet: %s", text)
	}

	posts := f.postCount()
	for _, c := range []struct {
		label  string
		args   map[string]any
		reason string
	}{
		{"подсеть другой зоны", map[string]any{"zone": "ru.AZ-1", "subnet_id": "sn-2"}, "CONFIG_INVALID"},
		{"подсети нет в проекте", map[string]any{"zone": "ru.AZ-1", "subnet_id": "sn-none"}, "CONFIG_INVALID"},
		{"неизвестная зона", map[string]any{"zone": "ru.AZ-9"}, "USAGE"},
	} {
		for _, tool := range []string{"k8s_plan", "k8s_create"} {
			args := map[string]any{"cluster_sa_id": "sa-1", "confirmed": true}
			for k, v := range c.args {
				args[k] = v
			}
			m, text, isErr := callNamed(t, r, tool, args)
			if !isErr {
				t.Errorf("%s %s: accepted: %s", tool, c.label, text)
				continue
			}
			wantReason(t, tool+" "+c.label, m, text, c.reason)
		}
	}
	if n := f.postCount(); n != posts {
		t.Errorf("refused calls sent %d POST", n-posts)
	}

	// В зоне нет подсети: раньше бралась первая подсеть проекта (другой зоны).
	f2 := &raAPI{}
	r2 := raRegistry(t, f2, raCloud{subnets: []*provision.Subnet{{ID: "sn-2", AvailabilityZoneID: raAZ2}}}, nil)
	m, text, isErr = callNamed(t, r2, "k8s_create", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1", "confirmed": true})
	if !isErr || f2.postCount() != 0 {
		t.Fatalf("no subnet in the zone: %s", text)
	}
	wantReason(t, "no subnet in zone", m, text, "CONFIG_INVALID")
}

// L-27: master_count=2 молча становился 1; теперь USAGE до гейта и до сети.
func TestK8s_MasterCountRefused(t *testing.T) {
	f := &raAPI{}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	for _, tool := range []string{"k8s_plan", "k8s_create"} {
		for _, confirmed := range []bool{false, true} {
			m, text, isErr := callNamed(t, r, tool, map[string]any{"master_count": float64(2), "confirmed": confirmed})
			if !isErr || !strings.Contains(text, "1, 3 or 5") {
				t.Errorf("%s confirmed=%v: %s", tool, confirmed, text)
			}
			wantReason(t, tool, m, text, "USAGE")
		}
	}
	if len(f.reqs) != 0 {
		t.Errorf("requests before the refusal: %v", f.reqs)
	}
	for _, n := range []float64{1, 3, 5} {
		if err := k8sArgsError(map[string]any{"master_count": n}); err != nil {
			t.Errorf("master_count %v: %v", n, err)
		}
	}
}

// L-10: превью k8s_add_nodepool без имени показывало пустое имя.
func TestK8sAddNodePool_PreviewShowsDefaultName(t *testing.T) {
	r := raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, nil)
	m, text, _ := callNamed(t, r, "k8s_add_nodepool", map[string]any{"cluster_id": "c-1"})
	p, _ := m["preview"].(map[string]any)
	if p["name"] != "nodepool-1" {
		t.Errorf("preview name %v, want nodepool-1: %s", p["name"], text)
	}
	m, _, _ = callNamed(t, r, "k8s_add_nodepool", map[string]any{"cluster_id": "c-1", "name": "workers"})
	if p, _ := m["preview"].(map[string]any); p["name"] != "workers" {
		t.Errorf("explicit name: %v", p["name"])
	}
}

// A pool of the name in the cluster is refused before the consent, with
// its id: a repeat after a lost answer made a second billable pool (fault
// matrix, process audit 1.2.1). A deleted pool frees its name.
func TestK8sAddNodePool_TakenNameRefusedBeforeConsent(t *testing.T) {
	f := &raAPI{pools: `{"id":"np-7","name":"workers","status":"RUNNING"},{"id":"np-8","name":"old","status":"DELETED"}`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	for _, confirmed := range []bool{false, true} {
		m, text, _ := callNamed(t, r, "k8s_add_nodepool", map[string]any{"cluster_id": "cl-1", "name": "workers", "confirmed": confirmed})
		wantReason(t, "taken name", m, text, "USAGE")
		if m["existing_id"] != "np-7" {
			t.Errorf("confirmed=%v: existing_id %v, want np-7: %s", confirmed, m["existing_id"], text)
		}
	}
	if n := f.postCount(); n != 0 {
		t.Errorf("%d creates sent for a taken name", n)
	}
	if m, text, _ := callNamed(t, r, "k8s_add_nodepool", map[string]any{"cluster_id": "cl-1", "name": "old"}); m["status"] != "confirmation_required" {
		t.Errorf("the name of a deleted pool is refused: %s", text)
	}
}

// L-26: без зоны в конфигурации lb_create слал пустой
// availability_zone_specs[0] (400), подсеть — первая в проекте. Теперь
// зона — из ВМ целевой группы (или аргумент zone), подсеть — главная в
// этой зоне и VPC группы; без зоны — USAGE до запроса.
func TestLbCreate_ZoneFromTargetGroup(t *testing.T) {
	f := &raAPI{targetVMs: `{"id":"vm-1","interface_id":"if-1","vpc_id":"vpc-1","zone_id":"` + raAZ1 + `","zone_name":"ru.AZ-1"}`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	base := map[string]any{"name": "web", "target_group_id": "tg-1", "port": float64(80)}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	m, text, _ := callNamed(t, r, "lb_create", base)
	wc, _ := m["will_create"].(map[string]any)
	if wc["zone"] != "ru.AZ-1" || wc["subnet_id"] != "sn-1" {
		t.Errorf("preview: zone %v subnet %v, want ru.AZ-1 sn-1: %s", wc["zone"], wc["subnet_id"], text)
	}
	if _, text, isErr := callNamed(t, r, "lb_create", with(map[string]any{"confirmed": true})); isErr {
		t.Fatalf("lb_create: %s", text)
	}
	specs, _ := f.post("/v2/networkLoadBalancers")["availability_zone_specs"].([]any)
	if len(specs) != 1 {
		t.Fatalf("availability_zone_specs: %v", specs)
	}
	az := specs[0].(map[string]any)
	if ids, _ := az["subnet_ids"].([]any); az["name"] != "ru.AZ-1" || len(ids) != 1 || ids[0] != "sn-1" {
		t.Errorf("availability_zone_specs[0] = %v", az)
	}

	// Аргумент zone — имя или UUID.
	for _, z := range []string{"ru.AZ-2", raAZ2} {
		m, text, _ = callNamed(t, r, "lb_create", with(map[string]any{"zone": z}))
		if wc, _ := m["will_create"].(map[string]any); wc["zone"] != "ru.AZ-2" || wc["subnet_id"] != "sn-2" {
			t.Errorf("zone=%s: %s", z, text)
		}
	}

	posts := f.postCount()
	for _, c := range []struct {
		label  string
		args   map[string]any
		reason string
	}{
		{"неизвестная зона", map[string]any{"zone": "ru.AZ-9"}, "USAGE"},
		{"подсеть не той зоны", map[string]any{"zone": "ru.AZ-1", "subnet_id": "sn-2"}, "CONFIG_INVALID"},
	} {
		m, text, isErr := callNamed(t, r, "lb_create", with(c.args))
		if !isErr {
			t.Errorf("%s: accepted: %s", c.label, text)
		}
		wantReason(t, c.label, m, text, c.reason)
	}

	// Группа без ВМ и без региона в конфигурации — USAGE, ничего не создано.
	f.targetVMs = ""
	m, text, isErr := callNamed(t, r, "lb_create", with(map[string]any{"confirmed": true}))
	if !isErr {
		t.Errorf("no zone: accepted: %s", text)
	}
	wantReason(t, "no zone", m, text, "USAGE")
	if n := f.postCount(); n != posts {
		t.Errorf("refused calls sent %d POST", n-posts)
	}
	// Регион конфигурации — запасной вариант.
	r2 := raRegistry(t, f, raCloud{subnets: raSubnets()}, func(c *config.Config) { c.Region = raAZ2 })
	m, text, _ = callNamed(t, r2, "lb_create", base)
	if wc, _ := m["will_create"].(map[string]any); wc["zone"] != "ru.AZ-2" || wc["subnet_id"] != "sn-2" {
		t.Errorf("region fallback: %s", text)
	}
}

// L-28: имя, зарезервированные порты и min>max проверяются до гейта;
// порт ≤ 1024 больше не включает privileged (root) молча; превью
// показывает все параметры и явную строку про root.
func TestCaDeploy_ValidatesAndPreviewsEverything(t *testing.T) {
	r := newTestServer()
	for _, c := range []map[string]any{
		{"name": "My_App"},
		{"name": "-app"},
		{"name": strings.Repeat("a", 64)},
		{"port": float64(80)},
		{"port": float64(1024)},
		{"port": float64(9090)},
		{"port": float64(9092)},
		{"port": float64(8012)},
		{"port": float64(70000)},
		{"min_instances": float64(3), "max_instances": float64(1)},
		{"min_instances": float64(-1)},
	} {
		args := map[string]any{"name": "app", "image": "r.cr.cloud.ru/app:1", "confirmed": true}
		for k, v := range c {
			args[k] = v
		}
		m, text, isErr := callNamed(t, r, "ca_deploy", args)
		if !isErr {
			t.Errorf("%v: accepted: %s", c, text)
		}
		wantReason(t, "ca_deploy", m, text, "USAGE")
	}

	m, text, _ := callNamed(t, r, "ca_deploy", map[string]any{"name": "app", "image": "r.cr.cloud.ru/app:1",
		"port": float64(8000), "env": map[string]any{"TOKEN": "secret-value"}})
	wc, _ := m["will_create"].(map[string]any)
	if wc["port"] != float64(8000) || wc["cpu"] != "0.1" || wc["memory"] != "256Mi" || wc["public"] != true ||
		wc["privileged"] != false || wc["min_instances"] != float64(0) || wc["max_instances"] != float64(1) ||
		!strings.Contains(stringArg(wc, "runs_as"), "UID 1000") {
		t.Errorf("preview: %s", text)
	}
	if strings.Contains(text, "secret-value") {
		t.Errorf("preview shows an env value: %s", text)
	}
	m, text, _ = callNamed(t, r, "ca_deploy", map[string]any{"name": "app", "image": "r.cr.cloud.ru/app:1", "privileged": true})
	if wc, _ := m["will_create"].(map[string]any); wc["privileged"] != true || !strings.Contains(stringArg(wc, "runs_as"), "root") ||
		!strings.Contains(stringArg(m, "message"), "root") {
		t.Errorf("privileged preview: %s", text)
	}

	// Порт 8080 по умолчанию: privileged в запрос не уходит.
	f := &raAPI{}
	r2 := raRegistry(t, f, raCloud{}, nil)
	if _, text, isErr := callNamed(t, r2, "ca_deploy", map[string]any{"name": "app", "image": "r.cr.cloud.ru/app:1", "confirmed": true}); isErr {
		t.Fatalf("ca_deploy: %s", text)
	}
	for path, b := range f.posts {
		if strings.Contains(b, "privileged") {
			t.Errorf("POST %s carries privileged: %s", path, b)
		}
	}
}

// A-024: k8s_helm с неизвестным action ходил в сеть (kubeconfig) до отказа.
func TestK8sHelm_BadActionRefusedBeforeNetwork(t *testing.T) {
	f := &raAPI{}
	r := raRegistry(t, f, raCloud{}, nil)
	for _, args := range []map[string]any{
		{"action": "explode"},
		{"action": "status"},
		{"action": "uninstall"},
		{"action": "install", "release": "r"},
	} {
		args["cluster_id"], args["confirmed"] = "c-1", true
		m, text, isErr := callNamed(t, r, "k8s_helm", args)
		if !isErr {
			t.Errorf("%v: accepted: %s", args, text)
		}
		wantReason(t, stringArg(args, "action"), m, text, "USAGE")
	}
	if len(f.reqs) != 0 {
		t.Errorf("requests before the refusal: %v", f.reqs)
	}
}

// L-29: описание ar_create называло аргумент type, а он registry_type.
func TestArCreate_DescriptionNamesRegistryType(t *testing.T) {
	tl, _ := newTestServer().Lookup("ar_create")
	if !strings.Contains(tl.Description, "registry_type=") || strings.Contains(tl.Description, " type=") {
		t.Errorf("ar_create description: %s", tl.Description)
	}
}

// L-36: стек с ВМ без CLOUDRU_IMAGE_ID и CLOUDRU_REGION на сервере всегда
// падал на шаге vm, уже создав предыдущие. Теперь vm.image и vm.zone
// (имя или UUID) в манифесте; stack_plan разрешает их в UUID и отказывает
// до создания чего-либо.
func TestStackPlan_VMImageAndZone(t *testing.T) {
	home := stackHome(t)
	r := raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, nil)
	plan := func(manifest string) (map[string]any, string, bool) {
		var mf map[string]any
		if err := json.Unmarshal([]byte(manifest), &mf); err != nil {
			t.Fatal(err)
		}
		return callNamed(t, r, "stack_plan", map[string]any{"manifest": mf})
	}
	for _, c := range []struct{ manifest, reason, want string }{
		{`{"name":"s","postgres":{"name":"p"},"vm":{"zone":"ru.AZ-1"}}`, "USAGE", "vm.image"},
		{`{"name":"s","postgres":{"name":"p"},"vm":{"image":"Ubuntu-24.04"}}`, "USAGE", "vm.zone"},
		{`{"name":"s","vm":{"image":"Ubuntu-24.04","zone":"ru.AZ-9"}}`, "USAGE", "ru.AZ-9"},
		{`{"name":"s","vm":{"image":"NoSuchImage","zone":"ru.AZ-1"}}`, "NOT_FOUND", "NoSuchImage"},
		{`{"name":"s","k8s":{"name":"k","master_count":2}}`, "USAGE", "1, 3 or 5"},
	} {
		m, text, isErr := plan(c.manifest)
		if !isErr || !strings.Contains(text, c.want) {
			t.Errorf("%s: %s", c.manifest, text)
		}
		wantReason(t, c.manifest, m, text, c.reason)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".evo", "stack-jobs")); len(entries) != 0 {
		t.Errorf("refused plans were saved: %d jobs", len(entries))
	}

	m, text, isErr := plan(`{"name":"s","vm":{"image":"Ubuntu-24.04","zone":"ru.AZ-1"}}`)
	if isErr {
		t.Fatalf("plan: %s", text)
	}
	if vm, _ := m["vm"].(map[string]any); vm["zone"] != "ru.AZ-1" || vm["image_id"] != raImage {
		t.Errorf("plan vm: %s", text)
	}
	j, err := stack.LoadJob(stringArg(m, "job_id"))
	if err != nil {
		t.Fatal(err)
	}
	args := j.Steps[0].Args
	if args["image_id"] != raImage || args["region"] != raAZ1 || args["image"] != nil {
		t.Errorf("vm step args: %v", args)
	}

	// Значения по умолчанию сервера (CLOUDRU_IMAGE_ID, CLOUDRU_REGION).
	r = raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, func(c *config.Config) { c.ImageID, c.Region = raImage, raAZ2 })
	m, text, isErr = plan(`{"name":"d","vm":{}}`)
	if vm, _ := m["vm"].(map[string]any); isErr || vm["zone"] != "ru.AZ-2" || vm["image"] != "Ubuntu-24.04" {
		t.Errorf("server defaults: %s", text)
	}
}

// L-37: stack_destroy отвечал destroyed, пока Postgres ещё удалялся
// (~4,5 мин); описание stack_apply обещало мгновенный ответ, а шаг ВМ
// блокирует ~3 мин.
func TestStackDestroy_ReportsAsyncDeletions(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"x","postgres":{"name":"p"},"secrets":[{"path":"a","value":"b"}]}`)
	for _, st := range j.Steps {
		st.Status = "done"
	}
	j.Status = "done"
	j.Outputs["postgres"], j.Outputs["secret:0"] = "pg-1", "a"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	r.handlers["pg_delete"] = fakeOK(map[string]any{"status": "deleting", "id": "pg-1", "operation_id": "op"})
	r.handlers["secret_delete"] = fakeOK(map[string]any{"status": "deleted"})
	got := callTool(t, r.handleStackDestroy, map[string]any{"job_id": j.ID, "confirmed": true})
	deleting, _ := got["deleting"].([]any)
	if got["status"] != "destroyed" || len(deleting) != 1 || deleting[0] != "postgres" || stringArg(got, "note") == "" {
		t.Errorf("destroy answer: %v", got)
	}

	tl, _ := r.Lookup("stack_apply")
	if strings.Contains(tl.Description, "Returns immediately") || !strings.Contains(tl.Description, "block") {
		t.Errorf("stack_apply description: %s", tl.Description)
	}
	tl, _ = r.Lookup("stack_destroy")
	if !strings.Contains(tl.Description, "deleting") {
		t.Errorf("stack_destroy description: %s", tl.Description)
	}
}

// R-04 (повторная приемка, N-11): превью stack_destroy перечисляет ресурсы
// стека, превью tg_delete и lb_delete называют ресурс по GET, а ID, которого
// нет, — ошибка до подтверждения.
func TestDeletePreviews_NameTheResource(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"x","postgres":{"name":"p"},"secrets":[{"path":"a","value":"b"}]}`)
	for _, st := range j.Steps {
		if st.Key == "postgres" {
			st.Status = "done"
		}
	}
	j.Outputs["postgres"] = "pg-1"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	got := callTool(t, r.handleStackDestroy, map[string]any{"job_id": j.ID})
	res, _ := got["resources"].([]any)
	if got["status"] != "confirmation_required" || len(res) != 1 || !strings.Contains(fmt.Sprint(res[0]), "pg-1") {
		t.Errorf("stack_destroy preview: %v", got)
	}

	f := &raAPI{targetVMs: `{"id":"vm-1"}`}
	rr := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	if _, text, _ := callNamed(t, rr, "tg_delete", map[string]any{"id": "tg-1"}); !strings.Contains(text, `\"tg\" (tg-1)`) {
		t.Errorf("tg_delete preview does not name the group: %s", text)
	}
	m, text, _ := callNamed(t, rr, "lb_delete", map[string]any{"id": "lb-missing"})
	if strings.Contains(text, "confirmation_required") {
		t.Fatalf("lb_delete preview of a missing balancer: %s", text)
	}
	wantReason(t, "lb_delete missing", m, text, "NOT_FOUND")
}

// Продолжение L-08 для групп узлов: зона по умолчанию была зоной продукта
// (ru.AZ-2), а подсеть — первой в проекте из любой зоны. Теперь зона —
// зона мастеров кластера, подсеть — из неё, чужая — CONFIG_INVALID.
func TestK8sAddNodePool_ClusterZoneSubnet(t *testing.T) {
	f := &raAPI{}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	args := map[string]any{"cluster_id": "cl-1", "security_group_id": "sg-1", "confirmed": true}
	if _, text, isErr := callNamed(t, r, "k8s_add_nodepool", args); isErr {
		t.Fatalf("k8s_add_nodepool: %s", text)
	}
	nc, _ := f.post("/v3/nodePools")["networkConfiguration"].(map[string]any)
	if id := nc["nodesSubnetId"]; id != "sn-1" && id != "sn-other-vpc" {
		t.Errorf("nodes subnet %v is not of the cluster's zone ru.AZ-1", id)
	}

	posts := f.postCount()
	args["nodes_subnet_id"] = "sn-2"
	m, text, isErr := callNamed(t, r, "k8s_add_nodepool", args)
	if !isErr {
		t.Fatalf("subnet of another zone accepted: %s", text)
	}
	wantReason(t, "nodes subnet of another zone", m, text, "CONFIG_INVALID")
	if f.postCount() != posts {
		t.Error("refused call sent a POST")
	}
}

// Ревью Grok по #89: зона кластера не прочиталась (ошибка GetCluster или
// пустой controlPlane.zones) — пул уходил в зону по умолчанию продукта,
// мимо плоскости управления (L-08). Теперь отказ до POST.
func TestK8sAddNodePool_ClusterZoneUnknownRefused(t *testing.T) {
	f := &raAPI{}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	for _, c := range []struct{ cluster, reason string }{
		{"cl-missing", "NOT_FOUND"},
		{"cl-nozone", "CONFIG_INVALID"},
	} {
		m, text, isErr := callNamed(t, r, "k8s_add_nodepool", map[string]any{"cluster_id": c.cluster, "security_group_id": "sg-1", "confirmed": true})
		if !isErr {
			t.Fatalf("%s: pool created without the cluster's zone: %s", c.cluster, text)
		}
		wantReason(t, c.cluster, m, text, c.reason)
	}
	if f.postCount() != 0 {
		t.Error("refused call sent a POST")
	}
}

// Финальный аудит, этап 2: без security_group_id и без группы в зоне
// кластера пул уходил без securityGroupId (спецификация это принимает).
// Теперь CONFIG_INVALID до POST; группа зоны подставляется сама.
func TestK8sAddNodePool_NoSecurityGroupInZoneRefused(t *testing.T) {
	f := &raAPI{}
	r := raRegistry(t, f, raCloud{subnets: raSubnets(),
		sgs: []*provision.SecurityGroup{{ID: "sg-az2", Name: "SSH-access_ru.AZ-2", AvailabilityZoneID: raAZ2}}}, nil)
	m, text, isErr := callNamed(t, r, "k8s_add_nodepool", map[string]any{"cluster_id": "cl-1", "confirmed": true})
	if !isErr {
		t.Fatalf("pool without a security group created: %s", text)
	}
	wantReason(t, "no SG in the cluster's zone", m, text, "CONFIG_INVALID")
	if f.postCount() != 0 {
		t.Error("refused call sent a POST")
	}

	r = raRegistry(t, f, raCloud{subnets: raSubnets(),
		sgs: []*provision.SecurityGroup{{ID: "sg-az1", Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: raAZ1}}}, nil)
	if _, text, isErr := callNamed(t, r, "k8s_add_nodepool", map[string]any{"cluster_id": "cl-1", "confirmed": true}); isErr {
		t.Fatalf("k8s_add_nodepool: %s", text)
	}
	nc, _ := f.post("/v3/nodePools")["networkConfiguration"].(map[string]any)
	if nc["securityGroupId"] != "sg-az1" {
		t.Errorf("securityGroupId = %v, want the cluster zone's group", nc["securityGroupId"])
	}
}

// Ревью Grok по #89: явный subnet_id другой VPC уходил в API как есть и
// получал 400 без имени поля; теперь CONFIG_INVALID до запроса.
func TestLbCreate_SubnetOfAnotherVPCRefused(t *testing.T) {
	f := &raAPI{targetVMs: `{"id":"vm-1","interface_id":"if-1","vpc_id":"vpc-1","zone_id":"` + raAZ1 + `","zone_name":"ru.AZ-1"}`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	m, text, isErr := callNamed(t, r, "lb_create", map[string]any{"name": "web", "target_group_id": "tg-1", "port": float64(80),
		"subnet_id": "sn-other-vpc", "confirmed": true})
	if !isErr {
		t.Fatalf("subnet of another VPC accepted: %s", text)
	}
	wantReason(t, "subnet of another VPC", m, text, "CONFIG_INVALID")
	if f.postCount() != 0 {
		t.Error("refused call sent a POST")
	}
}

// Повторная приемка #90, N-01: vpc_id не той VPC, что у группы целей,
// отказывал текстом про подсеть; теперь отказ про сам vpc_id до запроса.
func TestLbCreate_VPCOfAnotherTargetGroupRefused(t *testing.T) {
	f := &raAPI{targetVMs: `{"id":"vm-1","interface_id":"if-1","vpc_id":"vpc-1","zone_id":"` + raAZ1 + `","zone_name":"ru.AZ-1"}`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	m, text, isErr := callNamed(t, r, "lb_create", map[string]any{"name": "web", "target_group_id": "tg-1", "port": float64(80),
		"vpc_id": "vpc-other", "confirmed": true})
	if !isErr {
		t.Fatalf("vpc_id of another VPC accepted: %s", text)
	}
	wantReason(t, "vpc_id of another VPC", m, text, "CONFIG_INVALID")
	if !strings.Contains(text, "target group tg-1 is in VPC vpc-1") {
		t.Errorf("refusal does not name the target group's VPC: %s", text)
	}
	if f.postCount() != 0 {
		t.Error("refused call sent a POST")
	}
}

// Ревью Grok по #89: форма Redis (replicas=3 — прежнее документированное
// умолчание) проверялась только на шаге redis — после того как vpc, pg,
// kafka и k8s уже созданы. Теперь — USAGE на stack_plan.
func TestStackPlan_RedisShapeCheckedAtPlan(t *testing.T) {
	r := raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, nil)
	for _, c := range []struct {
		label    string
		manifest map[string]any
	}{
		{"redis.replicas=3", map[string]any{"name": "s1", "redis": map[string]any{"name": "r1", "replicas": float64(3)}}},
		{"redis.type=FOO", map[string]any{"name": "s1", "redis": map[string]any{"name": "r1", "type": "FOO"}}},
		{"kafka.brokers<0", map[string]any{"name": "s1", "kafka": map[string]any{"name": "k1", "brokers": float64(-1)}}},
	} {
		m, text, isErr := callNamed(t, r, "stack_plan", map[string]any{"manifest": c.manifest})
		if !isErr {
			t.Errorf("%s: planned: %s", c.label, text)
			continue
		}
		wantReason(t, c.label, m, text, "USAGE")
		if m["job_id"] != nil {
			t.Errorf("%s: job created: %s", c.label, text)
		}
	}
}

// Догфуд №4: «note» k8s в терминале называл инструменты MCP — «k8s_create
// … confirmed=true». Русский текст — CLI (MCP говорит по-английски, D6),
// поэтому в нём команды evo с ID кластера, а не имена инструментов;
// английский текст агента прежний. План называет flavor и именем (поле
// flavor), а не только UUID.
func TestK8s_NotesInCLIWords(t *testing.T) {
	f := &raAPI{}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	calls := []struct {
		tool   string
		args   map[string]any
		en, ru []string
	}{
		{"k8s_plan", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1"},
			[]string{"k8s_create", "confirmed=true", "k8s_get", "k8s_add_nodepool"},
			[]string{"evo k8s create", "--yes", "evo k8s get", "evo k8s add-nodepool"}},
		{"k8s_create", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1", "confirmed": true},
			[]string{"k8s_get", "k8s_kubeconfig"},
			[]string{"evo k8s get cl-1", "evo k8s kubeconfig cl-1"}},
		{"k8s_add_nodepool", map[string]any{"cluster_id": "cl-1", "security_group_id": "sg-1", "confirmed": true},
			[]string{"k8s_nodepools"},
			[]string{"evo k8s nodepools cl-1"}},
	}
	notes := func() []string {
		var out []string
		for _, c := range calls {
			m, text, isErr := callNamed(t, r, c.tool, c.args)
			note, _ := m["note"].(string)
			if isErr || note == "" {
				t.Fatalf("%s: %s", c.tool, text)
			}
			out = append(out, note)
		}
		return out
	}
	for i, note := range notes() {
		for _, want := range calls[i].en {
			if !strings.Contains(note, want) {
				t.Errorf("%s: no %q in the agent's note %q", calls[i].tool, want, note)
			}
		}
	}
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for i, note := range notes() {
		for _, want := range calls[i].ru {
			if !strings.Contains(note, want) {
				t.Errorf("%s: no %q in %q", calls[i].tool, want, note)
			}
		}
		if strings.Contains(note, "k8s_") || strings.Contains(note, "confirmed") {
			t.Errorf("%s: a tool's name in the CLI's note: %q", calls[i].tool, note)
		}
	}
	m, text, _ := callNamed(t, r, "k8s_plan", calls[0].args)
	if m["flavor"] != "gen-2-4" || m["flavor_id"] != "fl-1" {
		t.Errorf("flavor: %s", text)
	}
}

// D19 for stacks: an s3 object of the manifest with a bucket name S3 cannot
// take ("../x") passed stack_plan; stack_apply then created the VM and the
// rest, and only the s3 step refused it with USAGE — consent was given to a
// stack that could not finish. Now stack_plan refuses it, naming s3[i].bucket,
// before a job exists; a valid name plans as before.
func TestStackPlan_S3BucketNameCheckedAtPlan(t *testing.T) {
	r := raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, nil)
	for _, bad := range []string{"../x", "a/b"} {
		mf := map[string]any{"name": "s1", "s3": []any{map[string]any{"bucket": bad, "key": "k", "content": "c"}}}
		m, text, isErr := callNamed(t, r, "stack_plan", map[string]any{"manifest": mf})
		msg, _ := m["error"].(string)
		if !isErr || m["job_id"] != nil || !strings.Contains(msg, "s3[0].bucket") {
			t.Errorf("%q: %s", bad, text)
			continue
		}
		wantReason(t, bad, m, text, "USAGE")
	}
	mf := map[string]any{"name": "s1", "s3": []any{map[string]any{"bucket": "my-bucket", "key": "k", "content": "c"}}}
	if m, text, isErr := callNamed(t, r, "stack_plan", map[string]any{"manifest": mf}); isErr || m["job_id"] == nil {
		t.Errorf("valid bucket: %s", text)
	}
}

// cloud-e2e 22.10 (runs 39838101751, 39844928752): k8s_create sent the
// masters' flavor gen-16-128, the first in the catalog's order, not the
// smallest that fits — since #180 the loop's chosen name doubled as the
// explicit --flavor, and the first fit stopped the search. A cluster
// several times dearer than the one the plan meant, after a consent that
// named no flavor; the zone refused it as sold out.
func TestK8s_SmallestFittingFlavorWhateverCatalogOrder(t *testing.T) {
	f := &raAPI{flavors: `[
		{"flavorId":"fl-16-128","name":"gen-16-128","cpu":16,"ram":128},
		{"flavorId":"fl-gpu","name":"gpu-2-4","cpu":2,"ram":4,"gpu":1},
		{"flavorId":"fl-4-8","name":"gen-4-8","cpu":4,"ram":8},
		{"flavorId":"fl-2-2","name":"gen-2-2","cpu":2,"ram":2},
		{"flavorId":"fl-2-4","name":"gen-2-4","cpu":2,"ram":4}]`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	args := map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1"}
	m, text, isErr := callNamed(t, r, "k8s_plan", args)
	if isErr || m["flavor"] != "gen-2-4" || m["flavor_id"] != "fl-2-4" {
		t.Errorf("k8s_plan: want gen-2-4 (fl-2-4): %s", text)
	}
	args["confirmed"] = true
	if _, text, isErr := callNamed(t, r, "k8s_create", args); isErr {
		t.Fatalf("k8s_create: %s", text)
	}
	cp, _ := f.post("/v3/clusters")["controlPlaneConfiguration"].(map[string]any)
	sz, _ := cp["sizingConfiguration"].(map[string]any)
	if sz["flavorId"] != "fl-2-4" {
		t.Errorf("k8s_create sent flavor %v, want fl-2-4", sz["flavorId"])
	}
	// An explicit flavor is still taken by its name.
	m, text, _ = callNamed(t, r, "k8s_plan", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1", "flavor": "gen-4-8"})
	if m["flavor"] != "gen-4-8" || m["flavor_id"] != "fl-4-8" {
		t.Errorf("k8s_plan --flavor gen-4-8: %s", text)
	}
}
