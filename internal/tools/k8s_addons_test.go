// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// Плагины mk8s на поддельном API: формы ответов каталога — как у живого
// (api-probe #5), кластер и установка — по спецификации v3. Все UUID
// выдуманы.
const (
	addonProject = "00000000-0000-0000-0000-0000000000f1"
	addonCluster = "00000000-0000-0000-0000-0000000000c1"
)

// fakeMK8s отвечает на вызовы mk8s.api.cloud.ru: каталог из двух страниц,
// релизы cert-manager в произвольном порядке, кластер, группы узлов,
// clusterAddons (по умолчанию 400, как без clusterId на живом API).
type fakeMK8s struct {
	mu          sync.Mutex
	reqs        []string
	posts       []string
	kubeVersion string // "" — в ответе кластера версии нет
	pools       string // тело /v3/nodePools
	installed   string // тело /v3/clusterAddons; "" — 400
	releases    string // тело /v3/addons/cert-manager; "" — три релиза ниже
	created     string // ответ POST /v3/clusterAddons; "" — операция op-1
}

func (f *fakeMK8s) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	f.serve(rec, r)
	return rec.Result(), nil
}

func (f *fakeMK8s) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.Path)
	if r.URL.Host != "mk8s.api.cloud.ru" {
		http.Error(w, "unexpected host "+r.URL.Host, 500)
		return
	}
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v3/addons":
		if q.Get("projectId") != addonProject {
			http.Error(w, `{"message":"projectId is required"}`, 400)
			return
		}
		if q.Get("pageToken") == "" {
			_, _ = io.WriteString(w, `{"addons":[
				{"name":"cert-manager","displayName":"Cert Manager","description":"Выпуск TLS-сертификатов","categories":["Безопасность"],"isBillable":false},
				{"name":"traefik","displayName":"Traefik","description":"Ingress controller","categories":["Сети"],"isBillable":false}],
				"nextPageToken":"p2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"addons":[{"name":"gpu-operator","displayName":"GPU Operator","categories":["AI","Мониторинг"],"isBillable":true}]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/addons/cert-manager" && f.releases != "":
		_, _ = io.WriteString(w, f.releases)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/addons/cert-manager":
		_, _ = io.WriteString(w, `{"releases":[
			{"addonVersion":"1.0.0","appVersion":"v1.13.0","addonNamespace":"cert-manager","mode":"SUPPORT_MODE_ACTIVE","requirements":{"kubeVersions":">=1.25.0-0","readyWorkers":1}},
			{"addonVersion":"1.2.0","appVersion":"v1.15.0","addonNamespace":"cert-manager","mode":"SUPPORT_MODE_EXPERIMENTAL","requirements":{"kubeVersions":">=1.28.0-0","readyWorkers":1}},
			{"addonVersion":"1.1.0","appVersion":"v1.14.0","addonNamespace":"cert-manager","configurationSchema":"{\"type\":\"object\",\"properties\":{\"replicas\":{\"type\":\"integer\"}}}","mode":"SUPPORT_MODE_ACTIVE","requirements":{"kubeVersions":">=1.26.0-0","readyWorkers":"1"}}]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/clusters/"+addonCluster:
		body := map[string]any{"id": addonCluster, "name": "k8s-test", "status": "RUNNING"}
		if f.kubeVersion != "" {
			body["controlPlane"] = map[string]any{"version": f.kubeVersion}
		}
		_ = json.NewEncoder(w).Encode(body)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/nodePools":
		_, _ = io.WriteString(w, f.pools)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/clusterAddons":
		if f.installed == "" {
			http.Error(w, `{"message":"bad request"}`, 400)
			return
		}
		_, _ = io.WriteString(w, f.installed)
	case r.Method == http.MethodPost && r.URL.Path == "/v3/clusterAddons":
		b, _ := io.ReadAll(r.Body)
		f.posts = append(f.posts, string(b))
		if f.created != "" {
			_, _ = io.WriteString(w, f.created)
			return
		}
		_, _ = io.WriteString(w, `{"id":"op-1","resourceId":"ca-1","done":false}`)
	default:
		http.Error(w, `{"message":"not found"}`, 404)
	}
}

// addonRegistry — реестр с ключом, проектом и поддельным mk8s вместо
// http.DefaultTransport (клиент mk8s ходит через него).
func addonRegistry(t *testing.T, f *fakeMK8s) *Registry {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: addonProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

func TestK8sAddons_SearchAndFilters(t *testing.T) {
	r := addonRegistry(t, &fakeMK8s{})
	m, text, isErr := callNamed(t, r, "k8s_addons", nil)
	if isErr || m["total"] != float64(3) || m["shown"] != float64(3) {
		t.Fatalf("whole catalogue: %s", text)
	}
	rows := m["addons"].([]any)
	first := rows[0].(map[string]any)
	dw := first["deploy_with"].(map[string]any)
	if first["name"] != "cert-manager" || first["display_name"] != "Cert Manager" || first["billable"] != false ||
		dw["tool"] != "k8s_add_addon" || dw["args"].(map[string]any)["name"] != "cert-manager" {
		t.Errorf("row: %v", first)
	}
	if strings.Contains(text, "description") || strings.Contains(text, "Выпуск TLS") {
		t.Errorf("rows carry the description: %s", text)
	}
	if cats := m["categories"].([]any); len(cats) != 4 || cats[0] != "AI" {
		t.Errorf("categories: %v", cats)
	}

	names := func(args map[string]any) string {
		t.Helper()
		m, text, isErr := callNamed(t, r, "k8s_addons", args)
		if isErr {
			t.Fatalf("%v: %s", args, text)
		}
		var out []string
		for _, row := range m["addons"].([]any) {
			out = append(out, row.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"query": "CERT"}, "cert-manager"},
		{map[string]any{"query": "ingress"}, "traefik"},       // описание
		{map[string]any{"query": "tls-серт"}, "cert-manager"}, // описание по-русски, без учёта регистра
		{map[string]any{"category": "сети"}, "traefik"},
		{map[string]any{"category": "Мониторинг"}, "gpu-operator"},
		{map[string]any{"billable": true}, "gpu-operator"},
		{map[string]any{"billable": false}, "cert-manager,traefik"},
		{map[string]any{"query": "nothing-like-it"}, ""},
	} {
		if got := names(c.args); got != c.want {
			t.Errorf("%v: %q, want %q", c.args, got, c.want)
		}
	}
}

func TestK8sAddonVersions(t *testing.T) {
	r := addonRegistry(t, &fakeMK8s{})
	m, text, isErr := callNamed(t, r, "k8s_addon_versions", map[string]any{"name": "Cert-Manager"})
	if isErr {
		t.Fatal(text)
	}
	rels := m["releases"].([]any)
	var order []string
	for _, x := range rels {
		order = append(order, x.(map[string]any)["version"].(string))
	}
	if strings.Join(order, " ") != "v1.15.0 v1.14.0 v1.13.0" {
		t.Errorf("order: %v", order)
	}
	// Последняя — новейшая ACTIVE, не EXPERIMENTAL v1.15.0.
	if m["latest"] != "v1.14.0" || m["addon"] != "cert-manager" {
		t.Errorf("latest: %v", m["latest"])
	}
	r14 := rels[1].(map[string]any)
	if r14["latest"] != true || r14["mode"] != "ACTIVE" || r14["kube_versions"] != ">=1.26.0-0" || r14["ready_workers"] != float64(1) ||
		r14["namespace"] != "cert-manager" || r14["has_schema"] != true || r14["schema_bytes"] != float64(62) || r14["addon_version"] != "1.1.0" {
		t.Errorf("release: %v", r14)
	}
	if rels[0].(map[string]any)["mode"] != "EXPERIMENTAL" || rels[0].(map[string]any)["latest"] != false {
		t.Errorf("experimental release: %v", rels[0])
	}
	if strings.Contains(text, "configuration_schema") {
		t.Errorf("schema without full=true: %s", text)
	}
	if dw := m["deploy_with"].(map[string]any)["args"].(map[string]any); dw["version"] != "v1.14.0" {
		t.Errorf("deploy_with: %v", dw)
	}

	m, _, _ = callNamed(t, r, "k8s_addon_versions", map[string]any{"name": "cert-manager", "full": true})
	schema, _ := m["releases"].([]any)[1].(map[string]any)["configuration_schema"].(map[string]any)
	if schema["type"] != "object" {
		t.Errorf("full=true: schema %v", m["releases"].([]any)[1])
	}

	m, _, isErr = callNamed(t, r, "k8s_addon_versions", map[string]any{"name": "cert-manger"})
	hint, _ := m["hint"].(string)
	if !isErr || m["reason"] != clierrors.CodeNotFound || !strings.Contains(hint, "evo k8s addons") || !strings.Contains(hint, "cert-manager") {
		t.Errorf("unknown addon: %v", m)
	}
	if m, _, isErr := callNamed(t, r, "k8s_addon_versions", nil); !isErr || m["reason"] != clierrors.CodeUsage {
		t.Errorf("no name: %v", m)
	}
}

// Без confirmed — только чтения: превью с выбранной версией, namespace,
// требованиями и тем, что проверить не удалось.
func TestK8sAddAddon_PreviewOnlyReads(t *testing.T) {
	f := &fakeMK8s{pools: `{"nodePools":[{"clusterId":"` + addonCluster + `","scalePolicy":{"fixedScale":{"count":2}}}]}`}
	r := addonRegistry(t, f)
	m, text, isErr := callNamed(t, r, "k8s_add_addon", map[string]any{"cluster_id": addonCluster, "name": "cert-manager"})
	if !isErr || m["status"] != "confirmation_required" {
		t.Fatalf("first call: %s", text)
	}
	if len(f.posts) != 0 {
		t.Fatalf("preview posted: %v", f.posts)
	}
	// До подтверждения — только чтения (ревью Fable на #82).
	for _, req := range f.reqs {
		if !strings.HasPrefix(req, "GET ") {
			t.Errorf("preview sent %s", req)
		}
	}
	b, _ := json.Marshal(m["preview"])
	var p engine.CreatePreview
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{}
	for _, prm := range p.Params {
		params[prm.Key] = prm.Value
	}
	if p.Resource != engine.ResourceK8sAddon || p.Name != "cert-manager" || params["version"] != "v1.14.0" ||
		params["namespace"] != "cert-manager" || params["requires_kube"] != ">=1.26.0-0" || params["cluster_name"] != "k8s-test" ||
		params["worker_nodes"] != float64(2) || params["requires_workers"] != float64(1) || params["billable"] != false {
		t.Errorf("preview: %s", b)
	}
	// Версии кластера в ответе нет, список плагинов кластера — 400:
	// не проверено, но и не отказ.
	nc, _ := params["not_checked"].([]any)
	if len(nc) != 2 || nc[0] != "kube_version" || nc[1] != "installed" {
		t.Errorf("not_checked: %v", params["not_checked"])
	}
	if m["version_from"] != "latest" {
		t.Errorf("version_from: %v", m["version_from"])
	}
}

func TestK8sAddAddon_InstallsWithConfig(t *testing.T) {
	f := &fakeMK8s{kubeVersion: "v1.30.2", pools: `{"nodePools":[{"scalePolicy":{"autoScale":{"minCount":"1","maxCount":"3"}}}]}`,
		installed: `{"clusterAddons":[{"id":"ca-0","name":"traefik"}]}`}
	r := addonRegistry(t, f)
	args := map[string]any{"cluster_id": addonCluster, "name": "cert-manager", "version": "1.13.0",
		"config": `{"replicas": 2, "clusterName": "{{clusterName}}"}`, "resolve_mode": "overwrite"}
	m, text, _ := callNamed(t, r, "k8s_add_addon", args)
	if m["status"] != "confirmation_required" || m["version_from"] != "requested" || !strings.Contains(text, `"installed"`) ||
		strings.Contains(text, "not_checked") || !strings.Contains(text, "clusterName,replicas") {
		t.Fatalf("preview: %s", text)
	}
	// В превью — режим, как его пишет человек; в запросе — значение API.
	if !strings.Contains(text, `"OVERWRITE"`) || strings.Contains(text, "RESOLVE_CONFLICTS_MODE_") {
		t.Errorf("preview resolve_mode: %s", text)
	}
	args["confirmed"] = true
	m, text, isErr := callNamed(t, r, "k8s_add_addon", args)
	if isErr || m["status"] != "installing" || m["operation_id"] != "op-1" || m["cluster_addon_id"] != "ca-1" || m["namespace"] != "cert-manager" {
		t.Fatalf("confirmed: %s", text)
	}
	if note, _ := m["note"].(string); !strings.Contains(note, `k8s_kubectl args="get pods -n cert-manager"`) || !strings.Contains(note, "remove it in the Cloud.ru console") {
		t.Errorf("note: %s", note)
	}
	if len(f.posts) != 1 {
		t.Fatalf("posts: %v", f.posts)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(f.posts[0]), &body)
	if body["clusterId"] != addonCluster || body["name"] != "cert-manager" || body["appVersion"] != "v1.13.0" ||
		body["resolveMode"] != "RESOLVE_CONFLICTS_MODE_OVERWRITE" || body["addonConfiguration"] != `{"clusterName":"{{clusterName}}","replicas":2}` {
		t.Errorf("body: %s", f.posts[0])
	}
}

// Живой API (16.10.2026) повторяет версию плагина в addonNamespace: такой
// namespace не называется ни в превью, ни в ответе; следующий шаг — поды
// во всех namespace, и подсказка про SNAT-шлюз при ImagePullBackOff.
func TestK8sAddAddon_LiveNamespaceShape(t *testing.T) {
	f := &fakeMK8s{kubeVersion: "v1.30.2", pools: `{"nodePools":[{"scalePolicy":{"fixedScale":{"count":1}}}]}`,
		releases: `{"releases":[{"addonVersion":"1.21.1-sbc.0","addonNamespace":"1.21.1-sbc.0","appVersion":"v1.21.1","mode":"SUPPORT_MODE_ACTIVE","requirements":{"readyWorkers":1}}]}`}
	r := addonRegistry(t, f)
	args := map[string]any{"cluster_id": addonCluster, "name": "cert-manager"}
	m, text, _ := callNamed(t, r, "k8s_add_addon", args)
	if m["status"] != "confirmation_required" || strings.Contains(text, `"namespace"`) ||
		!strings.Contains(m["message"].(string), "the addon's default namespace") {
		t.Fatalf("preview: %s", text)
	}
	args["confirmed"] = true
	m, text, isErr := callNamed(t, r, "k8s_add_addon", args)
	if isErr || m["status"] != "installing" {
		t.Fatalf("confirmed: %s", text)
	}
	if _, ok := m["namespace"]; ok {
		t.Errorf("namespace in the answer: %s", text)
	}
	note, _ := m["note"].(string)
	for _, want := range []string{`k8s_kubectl args="get pods -A"`, "evo k8s kubectl " + addonCluster + " get pods -A", "Ready=True",
		"cert-manager-startupapicheck", "ImagePullBackOff", "SNAT gateway", "overview__limitations"} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q: %s", want, note)
		}
	}
}

// Требование, которое точно не выполнено, — отказ USAGE до подтверждения
// и без POST.
func TestK8sAddAddon_RefusesUnmetRequirements(t *testing.T) {
	cases := []struct {
		name string
		f    *fakeMK8s
		want string
	}{
		{"old kubernetes", &fakeMK8s{kubeVersion: "v1.25.9", pools: `{"nodePools":[{"scalePolicy":{"fixedScale":{"count":3}}}]}`}, ">=1.26.0-0"},
		{"no workers", &fakeMK8s{kubeVersion: "v1.30.2", pools: `{"nodePools":[]}`}, "1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := addonRegistry(t, c.f)
			for _, confirmed := range []bool{false, true} {
				m, text, isErr := callNamed(t, r, "k8s_add_addon", map[string]any{"cluster_id": addonCluster, "name": "cert-manager", "confirmed": confirmed})
				if !isErr || m["reason"] != clierrors.CodeUsage || !strings.Contains(m["error"].(string), c.want) ||
					!strings.Contains(m["hint"].(string), "addon-versions cert-manager") {
					t.Errorf("confirmed=%v: %s", confirmed, text)
				}
			}
			if len(c.f.posts) != 0 {
				t.Errorf("posted: %v", c.f.posts)
			}
		})
	}
}

func TestK8sAddAddon_BadArguments(t *testing.T) {
	f := &fakeMK8s{}
	r := addonRegistry(t, f)
	base := func(k string, v any) map[string]any {
		return map[string]any{"cluster_id": addonCluster, "name": "cert-manager", k: v}
	}
	for _, args := range []map[string]any{
		{"name": "cert-manager"},
		base("config", "not json"),
		base("config", "[1,2]"),
		base("config", []any{"a"}),
		base("resolve_mode", "merge"),
		// PRESERVE есть в enum спецификации, но к добавлению неприменим.
		base("resolve_mode", "PRESERVE"),
		base("resolve_mode", "RESOLVE_CONFLICTS_MODE_PRESERVE"),
	} {
		m, text, isErr := callNamed(t, r, "k8s_add_addon", args)
		if !isErr || m["reason"] != clierrors.CodeUsage {
			t.Errorf("%v: %s", args, text)
		}
	}
	if len(f.reqs) != 0 {
		t.Errorf("bad arguments reached the API: %v", f.reqs)
	}
	m, _, _ := callNamed(t, r, "k8s_add_addon", base("version", "9.9.9"))
	if m["reason"] != clierrors.CodeNotFound || !strings.Contains(m["hint"].(string), "addon-versions cert-manager") {
		t.Errorf("unknown version: %v", m)
	}
	m, _, _ = callNamed(t, r, "k8s_add_addon", base("name", "nope"))
	if m["reason"] != clierrors.CodeNotFound {
		t.Errorf("unknown addon: %v", m)
	}
}

func TestK8sAddonTools_GroupAndMutating(t *testing.T) {
	r := New(nilFactory)
	for name, mutating := range map[string]bool{"k8s_addons": false, "k8s_addon_versions": false, "k8s_add_addon": true} {
		tl, ok := r.Lookup(name)
		if !ok || tl.Group != "k8s" || tl.Mutating != mutating {
			t.Errorf("%s: group %q mutating %v", name, tl.Group, tl.Mutating)
		}
	}
	tl, _ := r.Lookup("k8s_add_addon")
	if !IsDestructive(tl) {
		t.Error("k8s_add_addon with resolve_mode OVERWRITE replaces objects: destructiveHint")
	}
}

// API ставит по appVersion и сам выбирает addonVersion (openapi-v3): версию
// плагина, которую сервер для этой версии приложения, скорее всего, не
// выберет, закрепить нельзя — USAGE, а не тихая подмена (ревью Grok на #78).
func TestK8sAddAddon_AddonVersionCannotBePinned(t *testing.T) {
	f := &fakeMK8s{kubeVersion: "v1.30.2", pools: `{"nodePools":[{"id":"np","scalePolicy":{"fixedScale":{"count":2}}}]}`,
		releases: `{"releases":[
			{"addonVersion":"1.1.0","appVersion":"v1.14.0","addonNamespace":"cert-manager","mode":"SUPPORT_MODE_ACTIVE","requirements":{"readyWorkers":1}},
			{"addonVersion":"1.1.1","appVersion":"v1.14.0","addonNamespace":"cert-manager","mode":"SUPPORT_MODE_ACTIVE","requirements":{"readyWorkers":1}}]}`}
	r := addonRegistry(t, f)
	args := func(v string) map[string]any {
		return map[string]any{"cluster_id": addonCluster, "name": "cert-manager", "version": v}
	}
	m, text, isErr := callNamed(t, r, "k8s_add_addon", args("1.1.0"))
	if !isErr || m["reason"] != clierrors.CodeUsage || !strings.Contains(m["hint"].(string), "--version v1.14.0") {
		t.Errorf("older addon version of a shared app version: %s", text)
	}
	for _, v := range []string{"1.1.1", "v1.14.0"} {
		_, text, _ = callNamed(t, r, "k8s_add_addon", args(v))
		if !strings.Contains(text, "confirmation_required") || !strings.Contains(text, "the server picks one") {
			t.Errorf("version %s: %s", v, text)
		}
	}
	if len(f.posts) != 0 {
		t.Errorf("installed without confirmation: %v", f.posts)
	}
}

// Неуспешная операция установки: ошибка по контракту, и operation_id в ней
// остаётся — агент может найти операцию (ревью Grok на #82).
func TestK8sAddAddon_FailedOperationKeepsID(t *testing.T) {
	f := &fakeMK8s{kubeVersion: "v1.30.2", pools: `{"nodePools":[{"scalePolicy":{"fixedScale":{"count":1}}}]}`,
		created: `{"id":"op-3","done":true,"error":{"code":9,"message":"addon install failed"}}`}
	r := addonRegistry(t, f)
	m, text, isErr := callNamed(t, r, "k8s_add_addon", map[string]any{"cluster_id": addonCluster, "name": "cert-manager", "confirmed": true})
	if !isErr || m["operation_id"] != "op-3" || m["reason"] == nil || !strings.Contains(m["error"].(string), "addon install failed") {
		t.Fatalf("answer: %s", text)
	}
}

// k8s_nodepools без cluster_id: USAGE с подсказкой k8s_list, без запроса
// к API (GET /v3/nodePools требует clusterId; раньше — API_ERROR,
// cloud-e2e readonly #23).
func TestK8sNodePools_ClusterRequired(t *testing.T) {
	r := addonRegistry(t, &fakeMK8s{})
	m, text, isErr := callNamed(t, r, "k8s_nodepools", nil)
	if !isErr || m["reason"] != "USAGE" {
		t.Fatalf("want USAGE, got %s", text)
	}
	if h, _ := m["hint"].(string); !strings.Contains(h, "k8s_list") || !strings.Contains(h, "evo k8s list") {
		t.Errorf("hint %q does not name k8s_list / evo k8s list", h)
	}
	tl, _ := r.Lookup("k8s_nodepools")
	if req, _ := tl.InputSchema["required"].([]string); len(req) != 1 || req[0] != "cluster_id" {
		t.Errorf("schema required = %v, want [cluster_id]", tl.InputSchema["required"])
	}
}
