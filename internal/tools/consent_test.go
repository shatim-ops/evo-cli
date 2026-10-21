// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// NeedsConsent is the one decision behind the gates and evo mcp
// --read-only (spec §8 Q12): every tool is classified by its schema — a tool
// without confirmed never needs consent, one with it always does, but the
// partialConsent ones, whose rule is the gate of their handler. The rest of
// the cross-check, every tool against its handler on a fake cloud, is
// TestReadOnly_EveryTool (internal/cli).
func TestNeedsConsent_Classified(t *testing.T) {
	reg := New(func(map[string]any) (*engine.Engine, error) { return nil, errors.New("no engine in tests") })
	for name := range partialConsent {
		tl, ok := reg.Lookup(name)
		if !ok || !takesConsent(tl) {
			t.Errorf("partialConsent: %s is no tool that takes confirmed", name)
		}
	}
	for _, tl := range reg.Tools() {
		_, partial := partialConsent[tl.Name]
		if takesConsent(tl) != (tl.Mutating || IsSensitive(tl)) {
			t.Errorf("%s: takes confirmed %v, mutating %v, sensitive %v", tl.Name, takesConsent(tl), tl.Mutating, IsSensitive(tl))
		}
		if AlwaysNeedsConsent(tl) != (takesConsent(tl) && !partial) {
			t.Errorf("%s: AlwaysNeedsConsent %v", tl.Name, AlwaysNeedsConsent(tl))
		}
		if !partial && NeedsConsent(tl, map[string]any{}) != takesConsent(tl) {
			t.Errorf("%s: NeedsConsent without arguments %v, takes confirmed %v", tl.Name, NeedsConsent(tl, map[string]any{}), takesConsent(tl))
		}
	}

	// The rule of each partial tool is its handler's gate: without
	// confirmed, the handler answers confirmation_required exactly when
	// NeedsConsent says so.
	for _, c := range []struct {
		tool  string
		args  map[string]any
		needs bool
	}{
		{"k8s_kubectl", map[string]any{"args": "get pods -A"}, false},
		{"k8s_kubectl", map[string]any{"args": "logs -f deploy/web"}, false},
		{"k8s_kubectl", map[string]any{"args": "rollout history deploy/web"}, false},
		{"k8s_kubectl", map[string]any{"args": "rollout undo deploy/web"}, true},
		{"k8s_kubectl", map[string]any{"args": "delete pod web"}, true},
		{"k8s_kubectl", map[string]any{"args": "get -f https://example.com/x.yaml"}, true},
		{"k8s_kubectl", map[string]any{"args": "frobnicate"}, true},
		{"k8s_helm", map[string]any{"action": "list"}, false},
		{"k8s_helm", map[string]any{"action": "status", "release": "web"}, false},
		{"k8s_helm", map[string]any{"action": "INSTALL", "release": "web", "chart": "repo/web"}, true},
		{"k8s_helm", map[string]any{"action": "uninstall", "release": "web"}, true},
	} {
		tl, _ := reg.Lookup(c.tool)
		c.args["cluster_id"] = "c"
		if got := NeedsConsent(tl, c.args); got != c.needs {
			t.Errorf("%s %v: NeedsConsent %v, want %v", c.tool, c.args, got, c.needs)
		}
		text, _ := tl.Handler(context.Background(), c.args)
		if gated := strings.Contains(text, `"confirmation_required"`); gated != c.needs {
			t.Errorf("%s %v: the handler gates %v, NeedsConsent %v: %s", c.tool, c.args, gated, c.needs, text)
		}
	}
}
