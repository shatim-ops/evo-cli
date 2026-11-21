// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// The command of power the Compute client sends is a write of its call,
// counted by the client itself (httpretry.UserAgent) — the layer of
// intents keeps the record of a cut vm_start, vm_stop or vm_reboot by this
// count alone, not by a word of the tool's handler; a read is no write
// («Риск», conclusion 187, У2-5).
func TestSetVMPower_IsAWriteOfItsCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"vm-1","name":"web","state":"running"}`)
	}))
	defer srv.Close()
	a := NewCloudRuAdapter(nil, srv.URL) // the client evo builds: UserAgent
	a.SetToken("t")
	told := 0
	ctx := httpretry.CountWrites(context.Background(), func() (bool, error) { told++; return true, nil })
	if _, err := a.GetVM(ctx, "vm-1"); err != nil {
		t.Fatal(err)
	}
	if n, _ := httpretry.Writes(ctx); n != 0 || told != 0 {
		t.Fatalf("a read counted %d writes, told %d", n, told)
	}
	if err := a.SetVMPower(ctx, "vm-1", "reboot"); err != nil {
		t.Fatal(err)
	}
	if n, _ := httpretry.Writes(ctx); n != 1 || told != 1 {
		t.Errorf("the command of power counted %d writes, told %d; want 1 and 1", n, told)
	}
}

// The real SNAT adapter runs through the counting transport. A GET to the
// same endpoint never marks the create. Refusing the durable mark keeps the
// POST out of the server; losing its response after acceptance sends it once.
func TestCreateNATGateway_MarkBeforePOST(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("refuse-%t", refuse), func(t *testing.T) {
			var posts, reads, marks atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/nat-gateways" || r.URL.Query().Get("project_id") != "project" {
					t.Errorf("unexpected URL %s", r.URL)
				}
				switch r.Method {
				case http.MethodGet:
					reads.Add(1)
					io.WriteString(w, `{"items":[],"total":0}`)
				case http.MethodPost:
					posts.Add(1)
					if marks.Load() != 1 {
						t.Error("POST reached transport before mark")
					}
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				default:
					t.Errorf("unexpected method %s", r.Method)
				}
			}))
			defer srv.Close()
			a := NewCloudRuAdapter(nil, srv.URL)
			ctx := httpretry.CountWrites(context.Background(), func() (bool, error) {
				marks.Add(1)
				if refuse {
					return false, errors.New("intent unavailable")
				}
				return true, nil
			})
			if _, err := a.ListNATGateways(ctx, "project"); err != nil || reads.Load() != 1 || marks.Load() != 0 {
				t.Fatalf("list: reads=%d marks=%d err=%v", reads.Load(), marks.Load(), err)
			}
			_, err := a.CreateNATGateway(ctx, CreateNATGatewayRequest{Name: "gateway", ProjectID: "project", AvailabilityZoneID: "zone", NATType: NATTypePublicSNAT})
			want := int32(1)
			if refuse {
				want = 0
			}
			if err == nil || posts.Load() != want || marks.Load() != 1 {
				t.Fatalf("create: posts=%d marks=%d err=%v", posts.Load(), marks.Load(), err)
			}
		})
	}
}
