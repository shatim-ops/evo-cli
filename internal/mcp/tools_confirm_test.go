// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// Операции, которые создают платные ресурсы или меняют чужое состояние,
// обязаны сначала вернуть превью и только на повторный вызов с
// confirmed=true — пойти дальше. Первый вызов не должен доходить даже до
// сборки движка (а значит, до облака).
func TestNewGates_PreviewFirstThenAction(t *testing.T) {
	// s3_put строит клиента из окружения, а не через движок: без ключей он
	// отказывает локально, не выходя в сеть.
	t.Setenv("CLOUDRU_S3_ACCESS_KEY", "")
	t.Setenv("CLOUDRU_S3_SECRET_KEY", "")

	var engines int
	s := New(func(map[string]any) (*engine.Engine, error) {
		engines++
		return nil, errors.New("no engine in tests")
	}, "test")

	cases := []struct {
		tool string
		args map[string]any
		want string // what the preview must name
		// engines: how many engines one preview builds; the previews write
		// nothing (TestContract_GatePreviewDoesNotWrite). provision and
		// deploy are not here: an engine that cannot be built, or a
		// configuration that names no project, image or zone, fails their
		// call whatever the answer, so they refuse before the preview
		// (D15, D48; TestNewGates_DeployWithoutEngineRefuses below).
		engines int
	}{
		{"ca_start", map[string]any{"name": "app-1"}, "app-1", 0},
		{"ca_stop", map[string]any{"name": "app-1"}, "app-1", 0},
		{"cert_issue_le", map[string]any{"name": "site", "dns_names": "example.ru"}, "example.ru", 0},
		{"cert_import", map[string]any{"name": "byo", "server_certificate": "Y2VydA==", "private_key": "a2V5"}, "byo", 0},
		{"cert_renew", map[string]any{"certificate_id": "cert-1"}, "cert-1", 0},
		{"s3_put", map[string]any{"bucket": "bkt", "key": "k.txt", "content": "hello"}, "s3://bkt/k.txt", 0},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			h := s.handlers[c.tool]
			if h == nil {
				t.Fatalf("no handler for %s", c.tool)
			}
			engines = 0

			out, isErr := h(context.Background(), c.args)
			got := callTool(t, h, c.args)
			if got["status"] != "confirmation_required" {
				t.Fatalf("first call: want confirmation_required, got %s", out)
			}
			if !isErr {
				// Same contract as the existing 19 groups (see BACKLOG).
				t.Errorf("first call: isError = false, the existing gates return true")
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("preview does not name %q: %s", c.want, out)
			}
			// Two calls above (h and callTool).
			if want := 2 * c.engines; engines != want {
				t.Fatalf("preview built an engine %d times, want %d", engines, want)
			}
			if c.engines > 0 && !strings.Contains(out, "project_id") {
				t.Errorf("preview does not name the project: %s", out)
			}

			confirmed := map[string]any{"confirmed": true}
			for k, v := range c.args {
				confirmed[k] = v
			}
			out, _ = h(context.Background(), confirmed)
			if strings.Contains(out, "confirmation_required") {
				t.Fatalf("confirmed call still asks for confirmation: %s", out)
			}
			// s3_put reads its credentials from the profile through the
			// engine, like every other gated tool: reaching the engine
			// factory is reaching the action.
			if engines == 0 {
				t.Fatalf("confirmed call did not proceed to the action: %s", out)
			}
		})
	}
}

// deploy without an engine fails its confirmed call too: the preview
// refuses it with the factory's error instead of asking for consent (D48;
// it was a preview without a price).
func TestNewGates_DeployWithoutEngineRefuses(t *testing.T) {
	s := New(func(map[string]any) (*engine.Engine, error) {
		return nil, errors.New("no engine in tests")
	}, "test")
	out, isErr := s.handlers["deploy"](context.Background(), map[string]any{"flavor": "lowcost10-2-4"})
	if !isErr || strings.Contains(out, "confirmation_required") || !strings.Contains(out, "no engine in tests") {
		t.Fatalf("deploy without an engine: want the factory's error before the question, got %s", out)
	}
}

// Новые инструменты под подтверждением обязаны объявлять confirmed в схеме,
// иначе агент не узнает, как пройти гейт.
func TestNewGates_SchemaAdvertisesConfirmed(t *testing.T) {
	s := newTestServer()
	gated := map[string]bool{"deploy": true, "provision": true, "ca_start": true, "ca_stop": true,
		"cert_issue_le": true, "cert_import": true, "cert_renew": true, "s3_put": true}
	for _, tl := range s.ListTools() {
		if !gated[tl.Name] {
			continue
		}
		delete(gated, tl.Name)
		props, _ := tl.InputSchema["properties"].(map[string]any)
		if _, ok := props["confirmed"]; !ok {
			t.Errorf("%s: no confirmed property in the input schema", tl.Name)
		}
		if !strings.Contains(tl.Description, "confirmed=true") {
			t.Errorf("%s: description does not mention confirmed=true", tl.Name)
		}
	}
	for name := range gated {
		t.Errorf("tool %s not found", name)
	}
}
