// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Цена превью vm create из price-calculation (api-probe #4, 16.10.2026):
// превью и так читает облако (flavor, образ), расчёт цены — ещё одно
// чтение без побочных эффектов. ID поддельные.

// pricedCloud — previewCloud с ответом расчёта цены и, по желанию, образом
// маркетплейса.
type pricedCloud struct {
	previewCloud
	imageType string
	price     func() (*provision.PriceCalculation, error)
	reqs      *[]provision.PriceCalculationRequest
}

func (c pricedCloud) GetImage(ctx context.Context, id string) (*provision.ImageDetail, error) {
	img, err := c.previewCloud.GetImage(ctx, id)
	if img != nil {
		img.Type = c.imageType
	}
	return img, err
}

func (c pricedCloud) PriceCalculation(_ context.Context, _ string, req provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	if c.reqs != nil {
		*c.reqs = append(*c.reqs, req)
	}
	return c.price()
}

func probeCalc() (*provision.PriceCalculation, error) {
	return &provision.PriceCalculation{Flavor: 1022, Disks: 140.16, FloatingIPs: 146, TotalPriceHour: 1.792, TotalPriceMonth: 1308.16}, nil
}

func TestVMCreatePreview_PriceFromAPI(t *testing.T) {
	russian(t)
	var reqs []provision.PriceCalculationRequest
	withCloud(t, pricedCloud{price: probeCalc, reqs: &reqs, imageType: provision.ImageTypeMarketplace})
	_, errOut, code := execCLI(t, "vm", "create", "web")
	if code != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit %d, want 3:\n%s", code, errOut)
	}
	t.Logf("\n%s", errOut)
	assertHas(t, errOut, []string{
		"  цена:       ~1308.16 RUB в месяц, 1.79 в час (ВМ 1022.00 + диск 140.16 + floating IP 146.00), " +
			"по API расчёта цены Cloud.ru, а не счёт; без лицензии образа\n",
		"  лицензия:   образ маркетплейса: лицензии в цене нет (API расчёта цены отвечает за образ 0)\n",
	})
	if len(reqs) != 1 {
		t.Fatalf("%d price calls, want 1", len(reqs))
	}
	r := reqs[0]
	if r.FlavorID != pvFlavor || r.ImageID != pvImage || len(r.Disks) != 1 || r.Disks[0].Size != 20 ||
		r.Disks[0].DiskTypeID != "00000000-0000-0000-0000-0000000000e6" || len(r.FloatingIPs) != 1 {
		t.Errorf("request %+v", r)
	}
}

// --json: the price of the API under the field names of the estimate, with
// the new fields beside them.
func TestVMCreatePreview_PriceFromAPIJSON(t *testing.T) {
	withCloud(t, pricedCloud{price: probeCalc})
	out, _, code := execCLI(t, "vm", "create", "web", "--no-public-ip", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var env struct {
		Preview struct {
			Details struct {
				Preview engine.CreatePreview `json:"preview"`
			} `json:"details"`
		} `json:"preview"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	p := env.Preview.Details.Preview
	c := p.Cost
	if c == nil || c.Source != pricing.SourceAPI || c.TotalMonthlyRub != 1308.16 || c.TotalHourlyRub != 1.792 ||
		c.ComputeMonthlyRub != 1022 || c.DiskMonthlyRub != 140.16 || c.Breakdown == nil || c.PriceAPIError != "" || c.LicenseNote != "" {
		t.Fatalf("cost %+v:\n%s", c, out)
	}
	if p.CostNote == "" || p.CostNote == "evo rate-card estimate, not a bill" {
		t.Errorf("cost_note %q", p.CostNote)
	}
}

// 422: the estimate as before, and a line why the API price is missing.
func TestVMCreatePreview_Price422FallsBack(t *testing.T) {
	withCloud(t, pricedCloud{price: func() (*provision.PriceCalculation, error) {
		body := `{"detail":"sku_list[1]: (sku_identifier: cannot be blank.)."}`
		return nil, clerrors.Wrap(clerrors.CodeAPIError, "API returned status 422: "+body, "", &provision.StatusError{Status: 422, Body: body})
	}})
	out, _, code := execCLI(t, "vm", "create", "web", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	assertHas(t, out, []string{
		"  cost:      ~2184.00 RUB/month (compute 2044.00 + disk 140.00), evo rate-card estimate, not a bill\n",
		"  API price: none (HTTP 422: sku_list[1]: (sku_identifier: cannot be blank.).), the estimate is shown\n",
	})
}
