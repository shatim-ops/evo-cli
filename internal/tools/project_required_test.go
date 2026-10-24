// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Релизный аудит, R-06: без project_id чтения уходили в API без проекта и
// получали сырой 422 «query.project_id: Field required» — API_ERROR, код 5.
// Теперь инструмент с проектным API отвечает CONFIG_INVALID (код 2) с
// подсказкой, как задать проект, и в облако ничего не уходит. Каталоги,
// инструменты по id и локальное состояние работают без проекта, как раньше.

// noProjectRegistry — настройки без проекта; каждый запрос к облаку
// записывается.
func noProjectRegistry(t *testing.T) (*Registry, *recorder) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	const id = "00000000-0000-0000-0000-000000000001"
	rec := &recorder{body: `{"id":"` + id + `","name":"x","items":[{"id":"` + id + `","name":"ru.AZ-2"}],"total":1,"projects":[]}`}
	old := http.DefaultTransport
	http.DefaultTransport = rec
	t.Cleanup(func() { http.DefaultTransport = old })
	s3srv := httptest.NewServer(rec)
	t.Cleanup(s3srv.Close)
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{Region: id, ImageID: id, KeyID: "k", Secret: "s",
				S3: config.S3Config{Endpoint: s3srv.URL, AccessKey: "a", SecretKey: "b"}},
			Auth:     staticToken{},
			Cloud:    provision.NewCloudRuAdapter(&http.Client{Transport: rec}, ""),
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
		}, nil
	}), rec
}

// fullArgs fills every argument of the schema (project_id aside), so the
// handler gets past its own checks to the cloud.
func fullArgs(tl Tool) map[string]any {
	const id = "00000000-0000-0000-0000-000000000001"
	props, _ := tl.InputSchema["properties"].(map[string]any)
	args := map[string]any{}
	for k, p := range props {
		pm := p.(map[string]any)
		if enum, ok := pm["enum"].([]any); ok && len(enum) > 0 {
			args[k] = enum[0]
			continue
		}
		switch pm["type"] {
		case "string":
			args[k] = id
			if pm["format"] == "cidr" {
				args[k] = "10.0.0.0/16"
			}
		case "integer", "number":
			args[k] = float64(1)
		case "boolean":
			args[k] = true
		case "array":
			args[k] = []any{"80"}
		case "object":
			args[k] = map[string]any{"name": "x"}
		}
	}
	delete(args, "project_id")
	delete(args, "image_metadata") // an array of field objects
	// One argument of each "one of" set: two of them is USAGE before the
	// project check (vpc_route_create next hops, break r4 R4-11).
	for _, set := range tl.RequiredOneOf {
		for _, k := range set[1:] {
			delete(args, k)
		}
	}
	// image and image_id exclude each other (provision): USAGE before the
	// project check too.
	if _, ok := args["image_id"]; ok {
		delete(args, "image")
	}
	if _, ok := args["name"]; ok {
		args["name"] = "app1" // a name the API takes, not a UUID
	}
	// Values the tools' own checks accept (release audit L-28, L-34, L-14),
	// so the call reaches the project check.
	overrides := map[string]any{"port": float64(8080), "replicas": float64(2), "since": "1h", "resolve_mode": "NONE"}
	for k, v := range overrides {
		if _, ok := args[k]; ok {
			args[k] = v
		}
	}
	return args
}

func TestProjectTools_NoProjectIsConfigInvalidBeforeAnyRequest(t *testing.T) {
	r, rec := noProjectRegistry(t)
	want := clierrors.ProjectRequired()
	for name := range projectTools {
		tl, ok := r.Lookup(name)
		if !ok {
			t.Errorf("projectTools names %q, which is not a tool", name)
			continue
		}
		m, text, isErr := callNamed(t, r, name, fullArgs(tl))
		if !isErr || m["reason"] != clierrors.CodeConfigInvalid || m["error"] != want.Message || m["hint"] != want.Hint {
			t.Errorf("%s without a project: %.300s", name, text)
		}
		if reqs := rec.take(); len(reqs) > 0 {
			t.Errorf("%s without a project sent %v", name, reqs)
		}
	}
	// Подсказка называет все три способа задать проект.
	for _, s := range []string{"evo configure --project-id", "CLOUDRU_PROJECT_ID", "evo project use"} {
		if !strings.Contains(want.Hint, s) {
			t.Errorf("hint %q does not name %s", want.Hint, s)
		}
	}
}

// Проект в аргументе вызова (iam_service_accounts project_id=…) — вызов
// идёт, как раньше.
func TestProjectTools_ProjectArgumentIsEnough(t *testing.T) {
	r, rec := noProjectRegistry(t)
	_, text, _ := callNamed(t, r, "iam_service_accounts", map[string]any{"project_id": "00000000-0000-0000-0000-0000000000aa"})
	if strings.Contains(text, clierrors.ProjectRequired().Message) {
		t.Errorf("project_id argument ignored: %s", text)
	}
	if reqs := rec.take(); len(reqs) != 1 || !strings.Contains(reqs[0], "iam.api.cloud.ru") {
		t.Errorf("requests: %v", reqs)
	}
}

// Каталоги и чтения без проектного API не требуют проекта.
func TestProjectTools_CataloguesWorkWithoutProject(t *testing.T) {
	r, rec := noProjectRegistry(t)
	for _, name := range []string{"list_zones", "list_flavors", "list_images", "list_disk_types", "org_projects", "status", "list"} {
		m, text, isErr := callNamed(t, r, name, map[string]any{})
		if isErr || m["reason"] != nil {
			t.Errorf("%s without a project: %.300s", name, text)
		}
	}
	if len(rec.take()) == 0 {
		t.Error("the catalogues sent no request at all")
	}
	// Инструмент по id — тоже: vpc_get не передаёт проект.
	if _, text, _ := callNamed(t, r, "vpc_get", map[string]any{"id": "00000000-0000-0000-0000-000000000002"}); strings.Contains(text, clierrors.ProjectRequired().Message) {
		t.Errorf("vpc_get: %s", text)
	}
}

// Превью изменяющего инструмента собирается без движка и без проекта, как
// раньше: проверка проекта — там, где обработчик строит движок.
func TestProjectTools_PreviewStaysEngineFree(t *testing.T) {
	r, _ := noProjectRegistry(t)
	m, text, _ := callNamed(t, r, "ca_start", map[string]any{"name": "app"})
	if m["status"] != "confirmation_required" {
		t.Errorf("ca_start preview: %s", text)
	}
}
