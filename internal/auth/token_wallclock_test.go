// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

// G140-3: the expiry of a token is a time of the wall clock — the IAM's and
// the service's. A time.Now() reading also carries the monotonic clock, and
// time.Until compares by it; that clock stands still while the host sleeps
// or the hypervisor pauses the VM, so a long-lived evo mcp holds a token
// the services already refuse (401 "Jwt is expired", 55 times in the Grok
// run of 1.4.0, MCP only). Every token the provider hands out, fresh and
// cached, has no monotonic reading.
func TestIAMProvider_ExpiresAtHasNoMonotonicReading(t *testing.T) {
	srv, _ := newMockIAMServer(t, time.Hour)
	p := NewIAMProvider(testCreds, srv.Client(), nil)
	for _, when := range []string{"exchanged", "from the memory cache"} {
		tok, err := p.Token(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		// A time with a monotonic reading prints it as " m=±…".
		if s := tok.ExpiresAt.String(); strings.Contains(s, " m=") {
			t.Errorf("token %s: ExpiresAt %s carries a monotonic reading — strip it (ExpiresAt.Round(0)), so the expiry is checked by the wall clock", when, s)
		}
	}
}
