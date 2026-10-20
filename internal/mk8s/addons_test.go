// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Все ID выдуманы; формы ответов — как у живого API (api-probe #5).
const (
	fakeProject = "00000000-0000-0000-0000-0000000000f1"
	fakeCluster = "00000000-0000-0000-0000-0000000000c1"
)

type addonAPI struct {
	mu   sync.Mutex
	reqs []*http.Request
	body []string
}

func (a *addonAPI) last() *http.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reqs[len(a.reqs)-1]
}

func newAddonAPI(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*Client, *addonAPI) {
	t.Helper()
	rec := &addonAPI{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, r)
		rec.body = append(rec.body, string(b))
		rec.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return New("tok", fakeProject, srv.URL), rec
}

// Каталог читается по страницам до пустого nextPageToken; projectId — в
// camelCase и только он (без него API отвечает 400).
func TestListAddons_Paged(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v3/addons" || q.Get("projectId") != fakeProject || q.Get("project_id") != "" || q.Get("pageSize") != "100" {
			http.Error(w, `{"message":"bad request"}`, 400)
			return
		}
		switch q.Get("pageToken") {
		case "":
			_, _ = io.WriteString(w, `{"addons":[{"name":"cert-manager","displayName":"Cert Manager","description":"TLS","categories":["Безопасность"],"isBillable":false,"createdAt":"2024-01-01T00:00:00Z"}],"nextPageToken":"p2"}`)
		case "p2":
			_, _ = io.WriteString(w, `{"addons":[{"name":"traefik","displayName":"Traefik","categories":["Сети"],"isBillable":true}],"nextPageToken":""}`)
		default:
			http.Error(w, "unexpected token", 400)
		}
	})
	got, err := cli.ListAddons(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "cert-manager" || got[0].Categories[0] != "Безопасность" || !got[1].IsBillable || got[0].IsBillable {
		t.Fatalf("addons: %+v", got)
	}
	if n := len(rec.reqs); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
	if h := rec.last().Header.Get("Authorization"); h != "Bearer tok" {
		t.Errorf("Authorization: %q", h)
	}
}

// Сервер, который отдаёт один и тот же токен, не зацикливает клиент: два
// запроса и ошибка «список не полон» (break r4, R4-6; раньше повтор молча
// заканчивал список).
func TestListAddons_RepeatedTokenIsError(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"addons":[{"name":"a"}],"nextPageToken":"same"}`)
	})
	if _, err := cli.ListAddons(context.Background()); err == nil || !strings.Contains(err.Error(), `"same"`) {
		t.Fatalf("err = %v", err)
	}
	if n := len(rec.reqs); n != 2 {
		t.Errorf("%d requests, want 2 (stop at the repeated token)", n)
	}
}

func TestListAddons_Error(t *testing.T) {
	cli, _ := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"message":"projectId is required"}`)
	})
	if _, err := cli.ListAddons(context.Background()); err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "projectId is required") {
		t.Fatalf("err = %v", err)
	}
}

// Релизы: схема — строкой, readyWorkers — числом или строкой (int64 в
// protojson), режим с префиксом SUPPORT_MODE_.
func TestGetAddonReleases(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = io.WriteString(w, `{"releases":[{"addonVersion":"1.0.0","appVersion":"v1.14.0","addonNamespace":"cert-manager","configurationSchema":"{\"type\":\"object\"}","mode":"SUPPORT_MODE_ACTIVE","requirements":{"kubeVersions":">=1.26.0-0","readyWorkers":1}}],"nextPageToken":"n"}`)
			return
		}
		// Живой API 16.10: addonNamespace повторяет addonVersion.
		_, _ = io.WriteString(w, `{"releases":[{"addonVersion":"0.9.0-sbc.0","addonNamespace":"0.9.0-sbc.0","appVersion":"v1.13.0","mode":"SUPPORT_MODE_END_OF_LIFE","requirements":{"readyWorkers":"2"}},`+
			`{"addonVersion":"1","addonNamespace":"1","appVersion":"v1.12.0"},`+
			// Версия с «v» и без неё — одна и та же (как в parseSemver).
			`{"addonVersion":"2-sbc.0","addonNamespace":"2","appVersion":"v2"}]}`)
	})
	got, err := cli.GetAddonReleases(context.Background(), "cert-manager")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("releases: %+v", got)
	}
	r := got[0]
	if r.AppVersion != "v1.14.0" || r.AddonNamespace != "cert-manager" || r.ConfigurationSchema != `{"type":"object"}` ||
		r.Requirements.KubeVersions != ">=1.26.0-0" || r.Requirements.ReadyWorkers != 1 || NormalizeMode(r.Mode) != ModeActive {
		t.Errorf("release 0: %+v", r)
	}
	if got[1].AddonNamespace != "" || got[2].AddonNamespace != "" || got[3].AddonNamespace != "" {
		t.Errorf("a version-shaped addonNamespace kept: %q, %q, %q", got[1].AddonNamespace, got[2].AddonNamespace, got[3].AddonNamespace)
	}
	if got[1].Requirements.ReadyWorkers != 2 {
		t.Errorf("readyWorkers as a string: %+v", got[1].Requirements)
	}
	if p := rec.reqs[0].URL.Path; p != "/v3/addons/cert-manager" {
		t.Errorf("path %q", p)
	}
}

// Имя плагина из аргументов агента не уводит запрос на другой путь.
func TestGetAddonReleases_EscapesName(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"releases":[]}`)
	})
	_, _ = cli.GetAddonReleases(context.Background(), "../clusters")
	if p := rec.last().URL.EscapedPath(); p != "/v3/addons/..%2Fclusters" {
		t.Errorf("escaped path %q", p)
	}
}

// Установка: тело по спецификации; ответ — операция (как у других
// изменений mk8s) или сам ClusterAddon.
func TestCreateClusterAddon(t *testing.T) {
	answer := `{"id":"op-1","resourceId":"ca-1","done":false}`
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v3/clusterAddons" {
			http.Error(w, "unexpected", 400)
			return
		}
		_, _ = io.WriteString(w, answer)
	})
	req := CreateClusterAddonReq{ClusterID: fakeCluster, Name: "cert-manager", AppVersion: "v1.14.0",
		AddonConfiguration: `{"replicas":2,"name":"{{clusterName}}"}`, ResolveMode: "RESOLVE_CONFLICTS_MODE_NONE"}
	res, err := cli.CreateClusterAddon(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.OperationID != "op-1" || res.ID != "ca-1" {
		t.Errorf("operation answer: %+v", res)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(rec.body[0]), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"clusterId": fakeCluster, "name": "cert-manager", "appVersion": "v1.14.0",
		"addonConfiguration": `{"replicas":2,"name":"{{clusterName}}"}`, "resolveMode": "RESOLVE_CONFLICTS_MODE_NONE"}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, body[k], v)
		}
	}
	if rec.last().URL.Query().Get("projectId") != fakeProject {
		t.Errorf("no projectId: %s", rec.last().URL.RawQuery)
	}

	// Операция без done и resourceId (protojson опускает done:false) —
	// всё равно операция, а не плагин (ревью Fable на #82).
	answer = `{"id":"op-2"}`
	res, err = cli.CreateClusterAddon(context.Background(), req)
	if err != nil || res.OperationID != "op-2" || res.ID != "" {
		t.Errorf("bare operation answer: %+v, %v", res, err)
	}
	rec.body = rec.body[:1]
	// Плагин с одним name — плагин, не операция (ревью Grok на #82);
	// пустой ответ — ошибка, а не «installing» без id.
	answer = `{"id":"ca-3","name":"x"}`
	if res, err = cli.CreateClusterAddon(context.Background(), req); err != nil || res.ID != "ca-3" || res.OperationID != "" {
		t.Errorf("addon with a name only: %+v, %v", res, err)
	}
	answer = `{}`
	if _, err = cli.CreateClusterAddon(context.Background(), req); err == nil {
		t.Error("empty answer accepted")
	}
	// Ошибка без id: текст API не теряется (ревью Grok на #82).
	answer = `{"done":true,"error":{"message":"boom"}}`
	if _, err = cli.CreateClusterAddon(context.Background(), req); err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "may have started") {
		t.Errorf("error without id: %v", err)
	}
	rec.body = rec.body[:1]

	answer = `{"id":"ca-2","status":"CLUSTER_ADDON_STATUS_INSTALLING"}`
	res, err = cli.CreateClusterAddon(context.Background(), CreateClusterAddonReq{ClusterID: fakeCluster, Name: "x", AppVersion: "1"})
	if err != nil || res.ID != "ca-2" || res.OperationID != "" || res.Status != "CLUSTER_ADDON_STATUS_INSTALLING" {
		t.Errorf("cluster addon answer: %+v, %v", res, err)
	}
	// resolveMode обязателен (живой API 16.10: 400 «must be specified»):
	// без него уходит NONE; пустая конфигурация не уходит.
	if strings.Contains(rec.body[1], "addonConfiguration") || !strings.Contains(rec.body[1], `"resolveMode":"RESOLVE_CONFLICTS_MODE_NONE"`) {
		t.Errorf("body: %s", rec.body[1])
	}

	answer = `{"id":"op-3","done":true,"error":{"code":9,"message":"no ready workers"}}`
	if _, err := cli.CreateClusterAddon(context.Background(), req); err == nil || !strings.Contains(err.Error(), "no ready workers") {
		t.Errorf("failed operation: %v", err)
	}
}

func TestListClusterAddons_ByCluster(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"clusterAddons":[{"id":"ca-1","name":"cert-manager","status":"INSTALLED"}]}`)
	})
	got, err := cli.ListClusterAddons(context.Background(), fakeCluster)
	if err != nil || len(got) != 1 || got[0]["name"] != "cert-manager" {
		t.Fatalf("%v, %v", got, err)
	}
	q := rec.last().URL.Query()
	if q.Get("clusterId") != fakeCluster || q.Get("projectId") != fakeProject {
		t.Errorf("query %s", rec.last().URL.RawQuery)
	}
}
