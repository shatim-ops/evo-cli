// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"maps"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The second client opens the same local state, after the service took
// the request but before its list reflects the write.
func TestNetworkRepeat_SecondClientAfterLostPost(t *testing.T) {
	for _, c := range faultMatrixCases {
		switch c.tool {
		case "dns_record_create", "vpc_route_create", "snat_create":
		default:
			continue
		}
		t.Run(c.tool, func(t *testing.T) {
			api, first, dir, k, ctx := paidCreateWorld(t, c, 1000)
			second := New(first.newEngine)
			root, _ := first.stateDir()
			second.SetStateDir(root)
			api.failAt(k, faultLost)
			first.handlers[c.tool](ctx, maps.Clone(c.args))
			api.disarm()
			pending := intent.Pending(dir)
			if len(pending) != 1 || !pending[0].Sent || !pending[0].Ended || creates(api) != 1 {
				t.Fatalf("after accepted POST: pending=%+v writes=%d", pending, creates(api))
			}
			text, failed := second.handlers[c.tool](context.Background(), maps.Clone(c.args))
			if !failed || answerStatus(text) != "confirmation_required" || creates(api) != 1 {
				t.Fatalf("repeat: writes=%d answer=%s", creates(api), text)
			}
		})
	}
}

// A cancelled read cannot be evidence of a billable SNAT write.
func TestNetworkRepeat_SNATReadCut(t *testing.T) {
	for _, c := range faultMatrixCases {
		if c.tool != "snat_create" {
			continue
		}
		api, r, dir, k, ctx := paidCreateWorld(t, c, 1000)
		api.failAt(k-1, faultCancel)
		text, failed := r.handlers[c.tool](ctx, maps.Clone(c.args))
		if !failed || creates(api) != 0 || len(intent.Pending(dir)) != 0 {
			t.Fatalf("cancelled read: writes=%d pending=%+v answer=%s", creates(api), intent.Pending(dir), text)
		}
		return
	}
	t.Fatal("missing SNAT world")
}
