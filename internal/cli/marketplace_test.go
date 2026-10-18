// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// evo marketplace list: поддельный каталог образов (imageCloud) и mk8s
// (addonAPI) сразу. Все UUID выдуманы.

func withMarketplace(t *testing.T, mk8s http.RoundTripper) imageCloud {
	t.Helper()
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	t.Setenv("CLOUDRU_REGION", "")
	cloud := newImageCloud()
	oldRT := http.DefaultTransport
	http.DefaultTransport = mk8s
	old := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth, e.Cloud = previewToken{}, cloud
		return e, nil
	}
	t.Cleanup(func() { toolEngine, http.DefaultTransport = old, oldRT })
	return cloud
}

// mk8sDown отвечает 500 на всё.
type mk8sDown struct{}

func (mk8sDown) RoundTrip(*http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	http.Error(w, `{"message":"internal"}`, 500)
	return w.Result(), nil
}

func TestMarketplaceList_RussianTable(t *testing.T) {
	russian(t)
	cloud := withMarketplace(t, &addonAPI{})
	cloud.hidden[imgID(1)] = true
	out, errOut, code := execCLI(t, "marketplace", "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	t.Logf("\n%s", out)
	assertHas(t, out, []string{
		"ВИД        ИМЯ                   ОТОБРАЖАЕМОЕ ИМЯ                КАК РАЗВЕРНУТЬ\n",
		"vm-os      alse-base-1-7-7       Astra Linux SE «Базовый» 1.7.7  evo vm create <имя-вм> --image alse-base-1-7-7\n",
		"vm-os      Ubuntu-24.04          Ubuntu 24.04                    evo vm create <имя-вм> --image Ubuntu-24.04\n",
		"vm-app     OpensearchDashboards  OpenSearch Dashboards           evo vm create <имя-вм> --image OpensearchDashboards\n",
		"k8s-addon  cert-manager          Cert Manager                    evo k8s add-addon <id-кластера> cert-manager\n",
		"Показаны 6 из 8: vm-os — 2, vm-app — 1, k8s-addon — 3.\n",
		"Скрыты образы: недоступны проекту: 1; не готовы ни в одной зоне: 1; показать — --all.\n",
		"Недоступные проекту образы есть в каталоге Cloud.ru",
		"(эвристика)",
		"в расчёте цены любой образ стоит 0",
	})
	// Порядок: по виду, затем по отображаемому имени.
	order := []string{"alse-base-1-7-7 ", "Ubuntu-24.04 ", "OpensearchDashboards ", "cert-manager ", "gpu-operator ", "traefik "}
	last := -1
	for _, name := range order {
		i := strings.Index(out, name)
		if i < last {
			t.Errorf("%q out of order:\n%s", name, out)
		}
		last = i
	}
	if strings.Contains(out, "AstraLinuxSmolensk1.8.3KES") || strings.Contains(out, "astra-voronej") {
		t.Errorf("hidden images shown:\n%s", out)
	}

	// --all: скрытые — с пометкой вместо команды.
	out, _, _ = execCLI(t, "marketplace", "list", "astra", "--all")
	t.Logf("\n%s", out)
	assertHas(t, out, []string{
		"vm-os  AstraLinuxSmolensk1.8.3KES  Astra Linux Смоленск 1.8.3 KES  недоступен проекту\n",
		"vm-os  astra-voronej               astra-voronej-cc11079           не готов ни в одной зоне\n",
		"Показаны 3 из 3: vm-os — 3, vm-app — 0, k8s-addon — 0.",
	})

	out, _, _ = execCLI(t, "marketplace", "list", "--kind", "k8s-addon", "tls")
	assertHas(t, out, []string{"cert-manager", "Показаны 1 из 1: vm-os — 0, vm-app — 0, k8s-addon — 1."})
	if strings.Contains(out, "эвристика") {
		t.Errorf("image note without images:\n%s", out)
	}

	out, _, code = execCLI(t, "marketplace", "list", "no-such-thing")
	if code != 0 {
		t.Fatalf("nothing found: exit %d", code)
	}
	assertHas(t, out, []string{"Ничего не найдено.", "evo marketplace list <подстрока>"})

	if _, errOut, code := execCLI(t, "marketplace", "list", "--kind", "vm"); code != clerrors.ExitGeneral || !strings.Contains(errOut, "kind") {
		t.Errorf("bad --kind: exit %d %s", code, errOut)
	}
}

// Каталог плагинов недоступен: образы показаны, предупреждение в stderr,
// код 0. --json — ответ инструмента.
func TestMarketplaceList_AddonsDownAndJSON(t *testing.T) {
	russian(t)
	withMarketplace(t, mk8sDown{})
	out, errOut, code := execCLI(t, "marketplace", "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	assertHas(t, out, []string{"Ubuntu-24.04", "k8s-addon — 0."})
	assertHas(t, errOut, []string{"каталог плагинов Managed Kubernetes не прочитан"})

	out, errOut, code = execCLI(t, "--json", "marketplace", "list", "ubuntu")
	if code != 0 {
		t.Fatalf("--json: exit %d: %s", code, errOut)
	}
	var got struct {
		Items    []map[string]any `json:"items"`
		Counts   map[string]int   `json:"counts"`
		Warnings []string         `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Items) != 1 || got.Counts["vm-os"] != 1 || len(got.Warnings) != 1 {
		t.Fatalf("%v: %s", err, out)
	}
	if d := got.Items[0]["deploy"].(map[string]any); d["tool"] != "provision" || got.Items[0]["available_in_project"] != true {
		t.Errorf("row: %v", got.Items[0])
	}

	// Только плагины, а mk8s недоступен, — ошибка.
	if _, _, code := execCLI(t, "marketplace", "list", "--kind", "k8s-addon"); code == 0 {
		t.Error("only addons, mk8s down: exit 0")
	}
}
