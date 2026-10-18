// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Every tool, called with no arguments (and with confirmed=true alone), on
// an engine factory that fails: every error answer has a reason (a code of
// internal/errors) and an error that does not start with "CODE: ", and a
// CLIError of the factory brings its code and hint (ревью r5 M5). Nothing
// reaches the network: the factory gives no engine.
func TestErrorContract_EveryTool(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	authErr := clierrors.New(clierrors.CodeAuthRequired, "no access key", "Run 'evo configure'")
	for _, f := range []struct {
		name    string
		factory EngineFactory
	}{
		{"plain error", nilFactory},
		{"CLIError", func(map[string]any) (*engine.Engine, error) { return nil, authErr }},
	} {
		r := New(f.factory)
		withCode := 0
		for _, tl := range r.Tools() {
			for _, args := range []map[string]any{{}, {"confirmed": true}} {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				text, isErr := tl.Handler(ctx, args)
				cancel()
				if !isErr {
					continue
				}
				var m map[string]any
				if err := json.Unmarshal([]byte(text), &m); err != nil {
					t.Errorf("%s %s %v: not JSON: %s", f.name, tl.Name, args, text)
					continue
				}
				if m["status"] == "confirmation_required" {
					continue
				}
				reason, _ := m["reason"].(string)
				msg, _ := m["error"].(string)
				if !clierrors.IsCode(reason) || msg == "" {
					t.Errorf("%s %s %v: reason %q, error %q", f.name, tl.Name, args, reason, msg)
				}
				if code, _, ok := strings.Cut(msg, ": "); ok && clierrors.IsCode(code) {
					t.Errorf("%s %s %v: error starts with a code: %q", f.name, tl.Name, args, msg)
				}
				if h, ok := m["hint"]; ok && h == "" {
					t.Errorf("%s %s %v: empty hint", f.name, tl.Name, args)
				}
				if f.name == "CLIError" && strings.Contains(msg, authErr.Message) &&
					(reason != clierrors.CodeAuthRequired || m["hint"] != authErr.Hint) {
					t.Errorf("%s %v: the factory's error lost its code or hint: %v", tl.Name, args, m)
				}
				if reason == clierrors.CodeAuthRequired {
					withCode++
				}
			}
		}
		if f.name == "CLIError" && withCode < 80 {
			t.Errorf("only %d answers carried the factory's code", withCode)
		}
	}
}
