// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
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

func TestNetworkRepeat_IdentityWithinParent(t *testing.T) {
	r := New(nil)
	for _, tc := range []struct {
		tool              string
		args, independent map[string]any
		equivalent        map[string]any
	}{
		{"dns_record_create", map[string]any{"type": "public", "zone_id": "z1", "name": "www", "record_type": "A"}, map[string]any{"zone_id": "z2"}, map[string]any{"record_type": "a", "ttl": 20}},
		{"dns_record_create", map[string]any{"type": "public", "zone_id": "z1", "name": "www", "record_type": "A"}, map[string]any{"record_type": "AAAA"}, map[string]any{"value": "192.0.2.1"}},
		{"vpc_route_create", map[string]any{"vpc_id": "v1", "subnet": "2001:db8::/64"}, map[string]any{"vpc_id": "v2"}, map[string]any{"subnet": "2001:0db8::/64", "description": "changed"}},
		{"vpc_route_create", map[string]any{"vpc_id": "v1", "subnet": "192.0.2.0/24"}, map[string]any{"subnet": "192.0.3.0/24"}, map[string]any{"next_hop_vip_id": "another"}},
	} {
		n, p, _ := r.resourceOf(context.Background(), tc.tool, tc.args)
		for i, patch := range []map[string]any{tc.independent, tc.equivalent} {
			a := maps.Clone(tc.args)
			maps.Copy(a, patch)
			nn, pp, _ := r.resourceOf(context.Background(), tc.tool, a)
			if same := n == nn && p == pp; same != (i == 1) {
				t.Errorf("%s: %q/%q -> %q/%q for %v", tc.tool, p, n, pp, nn, patch)
			}
		}
	}
}

func TestNetworkRepeat_SNATConflicts(t *testing.T) {
	for _, c := range faultMatrixCases {
		if c.tool != "snat_create" {
			continue
		}
		for _, tc := range []struct {
			label, name, id string
			want            int
		}{
			{"same zone, different name", "other", zoneAZ1, 1},
			{"same name, different zone", "evo-n", contractID, 1},
			{"different name and zone", "other", contractID, 2},
		} {
			t.Run(tc.label, func(t *testing.T) {
				api, r, _, k, ctx := paidCreateWorld(t, c, 1000)
				api.failAt(k, faultLost)
				r.handlers[c.tool](ctx, maps.Clone(c.args))
				api.disarm()
				args := maps.Clone(c.args)
				delete(args, "zone")
				args["name"], args["availability_zone_id"] = tc.name, tc.id
				text, _ := r.handlers[c.tool](context.Background(), args)
				if creates(api) != tc.want {
					t.Fatalf("writes=%d want=%d answer=%s", creates(api), tc.want, text)
				}
			})
		}
		return
	}
	t.Fatal("missing SNAT world")
}

func TestNetworkRepeat_FailedReadKeepsCut(t *testing.T) {
	for _, c := range faultMatrixCases {
		if c.tool != "dns_record_create" && c.tool != "vpc_route_create" && c.tool != "snat_create" {
			continue
		}
		t.Run(c.tool, func(t *testing.T) {
			api, r, dir, k, ctx := paidCreateWorld(t, c, 1000)
			api.failAt(k, faultLost)
			r.handlers[c.tool](ctx, maps.Clone(c.args))
			api.disarm()
			reject := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodGet {
					rec := httptest.NewRecorder()
					rec.WriteHeader(http.StatusForbidden)
					return rec.Result(), nil
				}
				return api.RoundTrip(req)
			})
			first := r.newEngine
			second := New(func(args map[string]any) (*engine.Engine, error) {
				eng, err := first(args)
				if err == nil {
					eng.Cloud = provision.NewCloudRuAdapter(&http.Client{Transport: reject}, "")
				}
				return eng, err
			})
			root, _ := r.stateDir()
			second.SetStateDir(root)
			httpretry.DefaultBase = reject
			_, failed := second.handlers[c.tool](context.Background(), maps.Clone(c.args))
			httpretry.DefaultBase = api
			if !failed || len(intent.Pending(dir)) != 1 || creates(api) != 1 {
				t.Fatalf("failed read: writes=%d records=%+v", creates(api), intent.Pending(dir))
			}
			text, _ := r.handlers[c.tool](context.Background(), maps.Clone(c.args))
			if creates(api) != 1 || answerStatus(text) != "confirmation_required" {
				t.Fatalf("after failed read: writes=%d answer=%s", creates(api), text)
			}
		})
	}
}
