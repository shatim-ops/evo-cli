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
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Дерево k8s: addons, addon-versions, add-addon на поддельном mk8s (формы
// каталога — как у живого API, api-probe #5). Все UUID выдуманы.

const addonClusterID = "00000000-0000-0000-0000-0000000000e1"

// addonAPI отвечает вместо mk8s.api.cloud.ru и запоминает тела POST.
type addonAPI struct {
	mu    sync.Mutex
	posts []string
}

func (a *addonAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case r.URL.Host != "mk8s.api.cloud.ru" || r.URL.Query().Get("projectId") == "" && r.URL.Query().Get("project_id") == "":
		http.Error(w, `{"message":"unexpected"}`, 400)
	case r.URL.Path == "/v3/addons":
		_, _ = io.WriteString(w, `{"addons":[
			{"name":"cert-manager","displayName":"Cert Manager","description":"TLS-сертификаты","categories":["Безопасность"],"isBillable":false},
			{"name":"traefik","displayName":"Traefik Ingress","categories":["Сети"],"isBillable":false},
			{"name":"gpu-operator","displayName":"GPU Operator","categories":["AI","Мониторинг"],"isBillable":true}]}`)
	case r.URL.Path == "/v3/addons/cert-manager":
		_, _ = io.WriteString(w, `{"releases":[
			{"addonVersion":"1.0.0","appVersion":"v1.13.0","addonNamespace":"cert-manager","mode":"SUPPORT_MODE_END_OF_SUPPORT","requirements":{"kubeVersions":">=1.25.0-0"}},
			{"addonVersion":"1.1.0","appVersion":"v1.14.0","addonNamespace":"cert-manager","configurationSchema":"{\"type\":\"object\",\"properties\":{\"replicas\":{\"type\":\"integer\"}}}","mode":"SUPPORT_MODE_ACTIVE","requirements":{"kubeVersions":">=1.26.0-0","readyWorkers":1}}]}`)
	case r.URL.Path == "/v3/clusters" && r.Method == http.MethodGet:
		_, _ = io.WriteString(w, `{"clusters":[]}`)
	case r.URL.Path == "/v3/clusters/"+addonClusterID:
		_, _ = io.WriteString(w, `{"id":"`+addonClusterID+`","name":"k8s-dev","version":"v1.31.4"}`)
	case r.URL.Path == "/v3/nodePools":
		_, _ = io.WriteString(w, `{"nodePools":[{"scalePolicy":{"fixedScale":{"count":2}}}]}`)
	case r.URL.Path == "/v3/clusterAddons" && r.Method == http.MethodGet:
		http.Error(w, `{"message":"bad request"}`, 400)
	case r.URL.Path == "/v3/clusterAddons" && r.Method == http.MethodPost:
		b, _ := io.ReadAll(r.Body)
		a.posts = append(a.posts, string(b))
		_, _ = io.WriteString(w, `{"id":"op-7","resourceId":"ca-7","done":false}`)
	default:
		http.Error(w, `{"message":"not found"}`, 404)
	}
	return w.Result(), nil
}

func withAddonAPI(t *testing.T) *addonAPI {
	t.Helper()
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	api := &addonAPI{}
	oldRT := http.DefaultTransport
	http.DefaultTransport = api
	old := toolEngine
	stoppedClock(t)
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth = previewToken{}
		return e, nil
	}
	t.Cleanup(func() { toolEngine, http.DefaultTransport = old, oldRT })
	return api
}

func TestK8sAddons_RussianTable(t *testing.T) {
	russian(t)
	withAddonAPI(t)
	out, errOut, code := execCLI(t, "k8s", "addons")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	t.Logf("\n%s", out)
	assertHas(t, out, []string{
		"ИМЯ           ОТОБРАЖАЕМОЕ ИМЯ  КАТЕГОРИИ       ПЛАТНЫЙ\n",
		"cert-manager  Cert Manager      Безопасность    нет\n",
		"gpu-operator  GPU Operator      AI, Мониторинг  да\n",
		"Показаны 3 из 3. Версии: evo k8s addon-versions <имя>; установка: evo k8s add-addon <id-кластера> <имя>.\n",
	})

	out, _, _ = execCLI(t, "k8s", "addons", "tls")
	assertHas(t, out, []string{"cert-manager", "Показаны 1 из 3."})
	out, _, _ = execCLI(t, "k8s", "addons", "--category", "сети")
	assertHas(t, out, []string{"traefik", "Показаны 1 из 3."})
	out, _, _ = execCLI(t, "k8s", "addons", "--billable")
	assertHas(t, out, []string{"gpu-operator", "Показаны 1 из 3."})

	out, _, code = execCLI(t, "k8s", "addons", "windows")
	if code != 0 {
		t.Fatalf("nothing found: exit %d", code)
	}
	assertHas(t, out, []string{"Плагины не найдены.", "evo k8s addons <подстрока>", "Категории (--category): AI, Безопасность, Мониторинг, Сети."})
}

func TestK8sAddons_JSON(t *testing.T) {
	withAddonAPI(t)
	out, errOut, code := execCLI(t, "--json", "k8s", "addons", "cert")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got struct {
		Addons []map[string]any `json:"addons"`
		Total  int              `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Total != 3 || len(got.Addons) != 1 {
		t.Fatalf("%v: %s", err, out)
	}
	if dw := got.Addons[0]["deploy_with"].(map[string]any); dw["tool"] != "k8s_add_addon" {
		t.Errorf("row: %v", got.Addons[0])
	}
}

func TestK8sAddonVersions_RussianTable(t *testing.T) {
	russian(t)
	withAddonAPI(t)
	out, errOut, code := execCLI(t, "k8s", "addon-versions", "cert-manager")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	t.Logf("\n%s", out)
	assertHas(t, out, []string{
		"cert-manager — Cert Manager\n",
		"ВЕРСИЯ     ВЕРСИЯ ПЛАГИНА  ПОДДЕРЖКА              KUBERNETES  УЗЛОВ  NAMESPACE     СХЕМА НАСТРОЕК\n",
		"v1.14.0 *  1.1.0           активная               >=1.26.0-0  1      cert-manager  62 Б\n",
		"v1.13.0    1.0.0           поддержка завершается  >=1.25.0-0  —      cert-manager  —\n",
		"* версия, которую add-addon ставит без --version",
	})
	if strings.Contains(out, "Схема настроек v1.14.0") {
		t.Errorf("schema without --full:\n%s", out)
	}
	out, _, _ = execCLI(t, "k8s", "addon-versions", "cert-manager", "--full")
	assertHas(t, out, []string{"Схема настроек v1.14.0:\n{\n  \"type\": \"object\","})

	// Неизвестный плагин — код 4 и подсказка с поиском по каталогу.
	isolateHome(t)
	code, _, errOut = runCLI(t, "", "k8s", "addon-versions", "cert-manger")
	if code != clerrors.ExitNotFound {
		t.Fatalf("unknown addon: exit %d, want 4: %s", code, errOut)
	}
	assertHas(t, errOut, []string{`плагин "cert-manger" не найден`, "evo k8s addons <подстрока>", "Похожие: cert-manager."})
}

func TestK8sAddAddon_ConfirmationAndConfigFile(t *testing.T) {
	russian(t)
	api := withAddonAPI(t)

	_, errOut, code := execCLI(t, "k8s", "add-addon", addonClusterID, "cert-manager")
	if code != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit %d, want 3: %s", code, errOut)
	}
	t.Logf("\n%s", errOut)
	assertHas(t, errOut, []string{
		"Будет установлено: плагин cert-manager в кластер",
		"имя кластера: k8s-dev",
		"версия:     v1.14.0",
		"пространство имён: cert-manager",
		"нужен Kubernetes: >=1.26.0-0",
		"Kubernetes кластера: v1.31.4",
		"узлов в группах: 2",
		"платный плагин: нет",
		"не проверено: установлен ли плагин уже",
	})
	assertNoJargon(t, errOut)

	if out, errOut, code := execCLI(t, "k8s", "add-addon", addonClusterID, "cert-manager", "--dry-run"); code != 0 || !strings.Contains(out+errOut, "версия:     v1.14.0") {
		t.Errorf("--dry-run: exit %d %s%s", code, out, errOut)
	}
	if len(api.posts) != 0 {
		t.Fatalf("installed without consent: %v", api.posts)
	}

	dir := t.TempDir()
	cfg := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(cfg, []byte(`{"replicas": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := execCLI(t, "--yes", "k8s", "add-addon", addonClusterID, "cert-manager", "--version", "v1.13.0", "--config", cfg)
	if code != 0 {
		t.Fatalf("--yes: exit %d: %s", code, errOut)
	}
	if len(api.posts) != 1 || !strings.Contains(api.posts[0], `"addonConfiguration":"{\"replicas\":2}"`) || !strings.Contains(api.posts[0], `"appVersion":"v1.13.0"`) {
		t.Errorf("posted: %v", api.posts)
	}
	assertHas(t, out, []string{"installing", "op-7"})

	// "-" — настройки из stdin.
	isolateHome(t)
	if code, _, errOut := runCLI(t, `{"replicas": 3}`, "--yes", "k8s", "add-addon", addonClusterID, "cert-manager", "--config", "-"); code != 0 {
		t.Fatalf("--config -: exit %d: %s", code, errOut)
	}
	if len(api.posts) != 2 || !strings.Contains(api.posts[1], `"addonConfiguration":"{\"replicas\":3}"`) {
		t.Errorf("--config -: %v", api.posts)
	}

	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte(`[1, 2]`), 0o600)
	for _, f := range []string{bad, filepath.Join(dir, "missing.json")} {
		if _, errOut, code := execCLI(t, "--yes", "k8s", "add-addon", addonClusterID, "cert-manager", "--config", f); code != clerrors.ExitGeneral || !strings.Contains(errOut, "--config") {
			t.Errorf("--config %s: exit %d %s", f, code, errOut)
		}
	}
	if len(api.posts) != 2 {
		t.Errorf("a bad --config reached the API: %v", api.posts)
	}
}

// isolateHome keeps runCLI (the real run, stderr with hints) away from the
// profiles of the machine running the tests.
func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
}
