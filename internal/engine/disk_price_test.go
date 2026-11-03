// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The preview of a disk is priced by the price-calculation API of Cloud.ru
// with the disk alone, as a VM's is with its flavor (Ф-1, agent run C2 on
// 1.6.0: the rate card priced an SSD ~39 % under the API). Without an
// answer it is the rate card's estimate for a disk, with the reason.
func TestDiskPreview_PriceFromAPI(t *testing.T) {
	var reqs []provision.PriceCalculationRequest
	cloud := &mockCloudAPI{priceFn: func(_ context.Context, _ string, req provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
		reqs = append(reqs, req)
		return &provision.PriceCalculation{Disks: 228.38, TotalPriceHour: 0.312, TotalPriceMonth: 228.38}, nil
	}}
	e := &Engine{Config: config.Config{ProjectID: "p"}, Cloud: cloud,
		Auth: &mockTokenProvider{token: auth.Token{Value: "t", ExpiresAt: time.Now().Add(time.Hour)}}}
	p := CreatePreview{Resource: ResourceDisk, Name: "data", ProjectID: "p", Zone: &deployplan.RefSpec{ID: testZoneID, Name: "zone-a"},
		Disk: &deployplan.DiskSpec{SizeGB: 20, Type: "SSD"}}
	if err := e.CompletePreview(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("%d price calls, want 1", len(reqs))
	}
	if r := reqs[0]; r.TotalCount != 1 || r.FlavorID != "" || r.ImageID != "" || len(r.FloatingIPs) != 0 ||
		len(r.Disks) != 1 || r.Disks[0].DiskTypeID != "dt-ssd" || r.Disks[0].Size != 20 {
		t.Errorf("request %+v: want the disk alone", r)
	}
	if p.Cost == nil || p.Cost.TotalMonthlyRub != 228.38 || p.Cost.DiskMonthlyRub != 228.38 || p.Cost.Source != pricing.SourceAPIDisk {
		t.Fatalf("cost %+v", p.Cost)
	}
	line := strings.Join(createCostLines(p), "\n")
	if !strings.Contains(line, "228.38") || !strings.Contains(line, "price-calculation") || strings.Contains(line, "rate-card") || strings.Contains(line, "license") {
		t.Errorf("cost line %q", line)
	}

	// No price from the API: the rate card for a disk, and why.
	cloud.priceFn = func(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
		return nil, errors.New("connection reset")
	}
	p = CreatePreview{Resource: ResourceDisk, Name: "data", ProjectID: "p", Disk: &deployplan.DiskSpec{SizeGB: 20, Type: "SSD"}}
	if err := e.CompletePreview(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Cost == nil || p.Cost.Source != pricing.SourceDiskCard || !strings.Contains(p.Cost.PriceAPIError, "connection reset") {
		t.Fatalf("fallback cost %+v", p.Cost)
	}
	line = strings.Join(createCostLines(p), "\n")
	if !strings.Contains(line, "connection reset") || strings.Contains(line, "vCPU") {
		t.Errorf("fallback cost line %q", line)
	}
}
