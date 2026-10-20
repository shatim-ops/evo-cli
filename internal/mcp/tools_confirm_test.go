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
		// engines: how many engines one preview builds. The previews of a
		// VM name the project it goes to (QA P6) and complete what they
		// create with reads (owner's decision Q6, spec REQ-7): provision
		// with the engine of its call; deploy with one for the project and
		// one for the new VM. With no engine (here) nothing is read; the
		// previews write nothing (TestContract_GatePreviewDoesNotWrite).
		engines int
	}{
		{"deploy", map[string]any{"flavor": "lowcost10-2-4"}, "lowcost10-2-4", 2},
		{"provision", map[string]any{"name": "vm-a", "no_public_ip": true}, "vm-a", 1},
		{"ca_start", map[string]any{"name": "app-1"}, "app-1", 0},
		{"ca_stop", map[string]any{"name": "app-1"}, "app-1", 0},
		{"cert_issue_le", map[string]any{"name": "site", "dns_names": "example.ru"}, "example.ru", 0},
		{"cert_import", map[string]any{"name": "byo", "server_certificate": "Y2VydA==", "private_key": "a2V5"}, "byo", 0},
		{"cert_renew", map[string]any{"certificate_id": "cert-1"}, "cert-1", 0},
		{"s3_put", map[string]any{"bucket": "b", "key": "k.txt", "content": "hello"}, "s3://b/k.txt", 0},
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
