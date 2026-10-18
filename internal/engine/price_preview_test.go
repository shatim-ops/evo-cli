// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The deploy preview prices the VM with the price-calculation API of
// Cloud.ru when it answers, else with the rate card and the reason. IDs are
// fake.

// probePrice is a 200 of price-calculation in the shape of the api-probe:
// the components per month.
func probePrice(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return &provision.PriceCalculation{Flavor: 1022, Disks: 140.16, FloatingIPs: 146, TotalPriceHour: 1.792, TotalPriceMonth: 1308.16}, nil
}

func dryRunPreview(t *testing.T, ctx context.Context, cloud func(*mockCloudAPI)) (string, *mockCloudAPI) {
	t.Helper()
	eng, mock, opts, stdout, _ := newDeployForConfirm(t)
	cloud(mock)
	opts.DryRun = true
	if err := eng.Deploy(ctx, opts); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if mock.createCalled {
		t.Fatal("--dry-run created a VM")
	}
	t.Logf("\n%s", stdout.String())
	return stdout.String(), mock
}

func TestDeployPreview_PriceFromAPI(t *testing.T) {
	var reqs []provision.PriceCalculationRequest
	out, _ := dryRunPreview(t, context.Background(), func(m *mockCloudAPI) {
		m.priceFn = func(ctx context.Context, project string, req provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
			reqs = append(reqs, req)
			return probePrice(ctx, project, req)
		}
	})
	if len(reqs) != 1 {
		t.Fatalf("%d price calls, want 1", len(reqs))
	}
	r := reqs[0]
	if r.FlavorID != "f-small" || r.ImageID == "" || len(r.Disks) != 1 || r.Disks[0].DiskTypeID != "dt-ssd" || r.Disks[0].Size != 10 ||
		len(r.FloatingIPs) != 1 || r.FloatingIPs[0].Action != "new" {
		t.Errorf("request %+v", r)
	}
	want := "  cost:      ~1308.16 RUB/month, 1.79/hour (VM 1022.00 + disk 140.16 + floating IP 146.00), " + apiCostNote + "\n"
	if !strings.Contains(out, want) {
		t.Errorf("preview lacks %q:\n%s", want, out)
	}
	for _, not := range []string{costNote, "API price:", "license:"} {
		if strings.Contains(out, not) {
			t.Errorf("preview has %q:\n%s", not, out)
		}
	}
}

// 422 of a marketplace image without a SKU in billing (seen live for two
// Astra images): the estimate, why the API price is missing, and that the
// license is not in the price.
func TestDeployPreview_Price422FallsBack(t *testing.T) {
	out, _ := dryRunPreview(t, context.Background(), func(m *mockCloudAPI) {
		m.priceFn = func(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
			body := `{"detail":"sku_list[1]: (sku_identifier: cannot be blank.)."}`
			return nil, clerrors.Wrap(clerrors.CodeAPIError, "API returned status 422: "+body, "", &provision.StatusError{Status: 422, Body: body})
		}
		m.getImageFn = func(ctx context.Context, id string) (*provision.ImageDetail, error) {
			img, _ := (&mockCloudAPI{}).GetImage(ctx, id)
			img.Type = provision.ImageTypeMarketplace
			return img, nil
		}
	})
	for _, want := range []string{
		"RUB/month (compute ", costNote,
		"  API price: none (HTTP 422: sku_list[1]: (sku_identifier: cannot be blank.).), the estimate is shown\n",
		"  license:   marketplace image: its license is not in the price (the rate card has no images)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}
}

// No answer in time: the estimate, without the deploy failing.
func TestDeployPreview_PriceTimeoutFallsBack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, _ := dryRunPreview(t, ctx, func(m *mockCloudAPI) {
		m.priceFn = func(ctx context.Context, _ string, _ provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
	})
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("waited %s", d)
	}
	for _, want := range []string{costNote, "  API price: none (no answer within "} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}
}
