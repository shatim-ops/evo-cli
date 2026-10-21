// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// TestBreak12_B12_1_KubectlShortNameSec: kubectl's short name for Secret
// is "sec" (sec/name, sec.v1, cm,sec). namesSecrets matches only "secret"
// and "secrets", so get -o yaml/json and describe of a Secret under that
// name are reads (NeedsConsent is false — H11 does not refuse them) and
// kubectlCheck returns nil on HTTP without --allow-secret-export and on
// a --read-only server. The stdout is one string, so H7 does not look
// inside it. "get sec -o name" stays allowed, as "get secret -o name" does:
// the table and -o name leave the data out.
func TestBreak12_B12_1_KubectlShortNameSec(t *testing.T) {
	leaks := []string{
		"get sec/db -o yaml",
		"get sec -o json",
		"describe sec",
		"get cm,sec -o yaml",
		"get sec.v1 -o yaml",
	}
	orig := binaryAvailable
	binaryAvailable = func(string) bool { return true }
	t.Cleanup(func() { binaryAvailable = orig })
	for _, mode := range []string{"read-only", "http"} {
		s := newTestServer()
		reached := 0
		s.wrapHandler("k8s_kubectl", func(tools.Handler) tools.Handler {
			return func(context.Context, map[string]any) (string, bool) {
				reached++
				return `{"output":"kind: Secret\ndata:\n  password: s3cret-value\n"}`, false
			}
		})
		if mode == "read-only" {
			s.SetReadOnly()
		} else {
			s.restrictForHTTP(false)
		}
		tl, ok := s.reg.Lookup("k8s_kubectl")
		if !ok {
			t.Fatal("k8s_kubectl")
		}
		// The word "secret" is already refused. A harness that lets it
		// through is not showing the short-name gap.
		before := reached
		text, isErr := call(s, "k8s_kubectl", map[string]any{"cluster_id": "c", "args": "get secret db -o yaml"})
		if reached > before || !isErr || strings.Contains(text, "s3cret-value") {
			t.Fatalf("%s: get secret -o yaml must already be refused, got %s", mode, text)
		}
		for _, cmd := range leaks {
			before = reached
			consent := tools.NeedsConsent(tl, map[string]any{"args": cmd})
			text, isErr = call(s, "k8s_kubectl", map[string]any{"cluster_id": "c", "args": cmd})
			if reached > before || !isErr || strings.Contains(text, "s3cret-value") {
				t.Errorf("%s %q: short name sec is a Secret and the call ran (NeedsConsent=%v, so H11 does not stop a read); H3 must refuse it before kubectl:\n%s",
					mode, cmd, consent, text)
			}
		}
		before = reached
		text, isErr = call(s, "k8s_kubectl", map[string]any{"cluster_id": "c", "args": "get sec -o name"})
		if reached == before || isErr {
			t.Errorf("%s: get sec -o name must stay allowed, as get secret -o name does: %s", mode, text)
		}
	}
}
