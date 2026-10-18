// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// The HTTP policy refuses with a reason too: USAGE for a call it does not
// allow in this mode, CONFIG_INVALID (and a hint) when kubectl/helm are not
// on the server (ревью r5 M5).
func TestHTTPPolicy_RefusalReasons(t *testing.T) {
	orig := binaryAvailable
	binaryAvailable = func(string) bool { return false }
	t.Cleanup(func() { binaryAvailable = orig })
	s := newTestServer()
	s.restrictForHTTP(false)
	for _, tt := range []struct {
		tool   string
		args   map[string]any
		reason string
		hint   bool
	}{
		{"secret_value", map[string]any{"path": "app/db"}, clierrors.CodeUsage, false},
		{"cert_private_key", map[string]any{"certificate_id": "c", "confirmed": true}, clierrors.CodeUsage, false},
		{"k8s_kubeconfig", map[string]any{"cluster_id": "c"}, clierrors.CodeUsage, false},
		{"deploy", map[string]any{"compose_file": "../x.yml"}, clierrors.CodeUsage, false},
		{"k8s_kubectl", map[string]any{"cluster_id": "c", "args": "get pods"}, clierrors.CodeConfigInvalid, true},
	} {
		text, isErr := call(s, tt.tool, tt.args)
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil || !isErr {
			t.Fatalf("%s: %v %s", tt.tool, isErr, text)
		}
		if m["status"] != "error" || m["reason"] != tt.reason || (m["hint"] != nil) != tt.hint {
			t.Errorf("%s: %v", tt.tool, m)
		}
	}
}
