// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
)

type fakeOrg struct {
	status int
	body   string
}

func (f fakeOrg) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	if r.URL.Host != "organization.api.cloud.ru" || r.URL.Path != "/v3/projects" {
		http.Error(rec, "unexpected "+r.URL.String(), 500)
		return rec.Result(), nil
	}
	rec.WriteHeader(f.status)
	_, _ = io.WriteString(rec, f.body)
	return rec.Result(), nil
}

func orgRegistry(t *testing.T, f fakeOrg) *Registry {
	t.Helper()
	return orgRegistryIn(t, f, addonProject)
}

func orgRegistryIn(t *testing.T, f fakeOrg, project string) *Registry {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: project, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

// Ключ уровня проекта: Organization API отвечает 400 «user not found».
// org_projects отвечал NOT_FOUND (код 4), evo project list — пояснением
// (QA r3 C3, cloud-e2e readonly #23). Теперь оба — без ошибки.
func TestOrgProjects_ProjectLevelKeyIsNotAnError(t *testing.T) {
	r := orgRegistry(t, fakeOrg{400, `{"code":3,"message":"(user_id) is invalid argument: user not found"}`})
	m, text, isErr := callNamed(t, r, "org_projects", nil)
	if isErr {
		t.Fatalf("isError for a project-level key: %s", text)
	}
	if v, ok := m["projects"]; !ok || v != nil {
		t.Errorf("projects = %v, want null", v)
	}
	if m["active"] != addonProject || m["note"] != ScopeText(0, false) || m["http_status"] != float64(400) {
		t.Errorf("answer %s", text)
	}
	if _, ok := m["reason"]; ok {
		t.Errorf("a reason in a non-error answer: %s", text)
	}
}

// Релизный аудит, R-13: проекта нет — подсказка не говорит «ключ работает в
// активном проекте», а говорит, где взять UUID и как его задать.
func TestOrgProjects_NoActiveProjectHint(t *testing.T) {
	r := orgRegistryIn(t, fakeOrg{400, `{"message":"user not found"}`}, "")
	m, text, isErr := callNamed(t, r, "org_projects", nil)
	if isErr || m["active"] != "" || m["hint"] != orgNoProjectHint {
		t.Fatalf("answer %s", text)
	}
	if h, _ := m["hint"].(string); strings.Contains(h, "works in the active project") || !strings.Contains(h, "evo configure --project-id") {
		t.Errorf("hint %q", h)
	}
	r = orgRegistry(t, fakeOrg{400, `{"message":"user not found"}`})
	if m, _, _ := callNamed(t, r, "org_projects", nil); !strings.Contains(m["hint"].(string), "works in the active project") {
		t.Errorf("with a project: %v", m["hint"])
	}
}

func TestOrgProjects_List(t *testing.T) {
	r := orgRegistry(t, fakeOrg{200, `{"projects":[{"id":"p1","name":"dev","status":"ACTIVE","organization_unit_id":"u"}]}`})
	m, text, isErr := callNamed(t, r, "org_projects", nil)
	if isErr {
		t.Fatalf("error: %s", text)
	}
	if ps, _ := m["projects"].([]any); len(ps) != 1 {
		t.Errorf("projects: %s", text)
	}
}

// Мягкий ответ — только 400/403/404 (ключ уровня проекта). Сбой
// Organization API (500) или отказ в аутентификации (401) — ошибка со
// своей причиной, а не «Not an error» (ревью Fable, A3).
func TestOrgProjects_FailuresAreErrors(t *testing.T) {
	for status, want := range map[int]string{
		403: "",
		404: "",
		500: "API_ERROR",
		401: "AUTH_FAILED",
	} {
		r := orgRegistry(t, fakeOrg{status, `{"message":"user not found"}`})
		m, text, isErr := callNamed(t, r, "org_projects", nil)
		if want == "" {
			if isErr || m["note"] != ScopeText(0, false) {
				t.Errorf("%d: %s", status, text)
			}
			continue
		}
		if !isErr || m["reason"] != want || m["note"] != nil {
			t.Errorf("%d: isErr=%v %s", status, isErr, text)
		}
	}
}
