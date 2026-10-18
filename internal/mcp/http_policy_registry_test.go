// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"strings"
	"testing"
)

// The HTTP policy is applied in the registry, so stack_apply, which calls
// other tools in-process, cannot reach a restricted tool around it.
func TestHTTPPolicy_AppliesToInProcessCalls(t *testing.T) {
	s := newTestServer()
	s.restrictForHTTP(false)
	tl, ok := s.reg.Lookup("cert_private_key")
	if !ok {
		t.Fatal("cert_private_key not in the registry")
	}
	text, isErr := tl.Handler(context.Background(), map[string]any{"certificate_id": "c", "confirmed": true})
	if !isErr || !strings.Contains(text, "--allow-secret-export") {
		t.Errorf("registry handler is not restricted: %s", text)
	}
}
