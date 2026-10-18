// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"strings"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Цена плана из price-calculation (api-probe #4, 16.10.2026): API ответил —
// цена его, не ответил (422 у образа Astra без SKU, тайм-аут) — оценка по
// карте с причиной. Все ID поддельные.

const priceDiskType = "00000000-0000-4000-8000-0000000000d1"

// priceCloud — metaCloud с типом диска, образом маркетплейса по желанию и
// расчётом цены по answer; запросы расчёта запоминаются.
type priceCloud struct {
	metaCloud
	imageType string
	answer    func(ctx context.Context) (*provision.PriceCalculation, error)
	reqs      []provision.PriceCalculationRequest
	projects  []string
}

func (c *priceCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return []*provision.Image{{ID: "img-u", Name: "Ubuntu-24.04", Type: c.imageType}}, nil
}
func (*priceCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, nil
}
func (*priceCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return []*provision.DiskType{{ID: priceDiskType, Name: "SSD"}}, nil
}
func (c *priceCloud) PriceCalculation(ctx context.Context, projectID string, req provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	c.reqs = append(c.reqs, req)
	c.projects = append(c.projects, projectID)
	return c.answer(ctx)
}

// probeAnswer — ответ 200 в форме пробы: составляющие за месяц.
func probeAnswer(context.Context) (*provision.PriceCalculation, error) {
	return &provision.PriceCalculation{Flavor: 1022, Disks: 140.16, FloatingIPs: 146, TotalPriceHour: 1.792, TotalPriceMonth: 1308.16}, nil
}

func resolvePlan(t *testing.T, cloud *priceCloud) *Plan {
	t.Helper()
	plan, err := Resolve(context.Background(), cloud, "proj-1", Params{Name: "web", SSHKey: "ssh-ed25519 AAAA agent", ExternalIP: true})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return plan
}

func TestResolve_PriceFromAPI(t *testing.T) {
	cloud := &priceCloud{metaCloud: metaCloud{fields: ubuntu2404Fields()}, answer: probeAnswer}
	plan := resolvePlan(t, cloud)

	if len(cloud.reqs) != 1 || cloud.projects[0] != "proj-1" {
		t.Fatalf("price calls %v in %v, want one in proj-1", cloud.reqs, cloud.projects)
	}
	req := cloud.reqs[0]
	if req.TotalCount != 1 || req.FlavorID != "fl-1" || req.ImageID != "img-u" ||
		len(req.Disks) != 1 || req.Disks[0] != (provision.PriceDisk{DiskTypeID: priceDiskType, Size: plan.VM.Disk.SizeGB}) ||
		len(req.FloatingIPs) != 1 || req.FloatingIPs[0] != (provision.PriceFloatingIP{Action: "new", Count: 1}) {
		t.Errorf("request %+v", req)
	}

	c := plan.Cost
	if c.Source != pricing.SourceAPI || c.PriceAPIError != "" {
		t.Fatalf("source %q, api error %q", c.Source, c.PriceAPIError)
	}
	if c.TotalMonthlyRub != 1308.16 || c.TotalHourlyRub != 1.792 || c.ComputeMonthlyRub != 1022 ||
		c.DiskMonthlyRub != 140.16 || c.FloatingIPMonthlyRub != 146 || c.Currency != "RUB" {
		t.Errorf("cost %+v", c)
	}
	if c.Breakdown == nil || c.Breakdown.Period != "month" || c.Breakdown.Flavor != 1022 {
		t.Errorf("breakdown %+v", c.Breakdown)
	}
	if c.LicenseNote != "" {
		t.Errorf("license note for a public image: %q", c.LicenseNote)
	}
}

// Без внешнего IP floating IP в расчёт не идёт.
func TestResolve_PriceWithoutFloatingIP(t *testing.T) {
	cloud := &priceCloud{metaCloud: metaCloud{fields: ubuntu2404Fields()}, answer: probeAnswer}
	if _, err := Resolve(context.Background(), cloud, "proj-1", Params{Name: "web", SSHKey: "ssh-ed25519 AAAA agent"}); err != nil {
		t.Fatal(err)
	}
	if len(cloud.reqs) != 1 || cloud.reqs[0].FloatingIPs != nil {
		t.Errorf("requests %+v", cloud.reqs)
	}
}

// 422 образа маркетплейса без SKU: план с оценкой по карте, причина — в
// price_api_error, лицензия не входит в цену.
func TestResolve_Price422FallsBackToEstimate(t *testing.T) {
	cloud := &priceCloud{metaCloud: metaCloud{fields: ubuntu2404Fields()}, imageType: provision.ImageTypeMarketplace,
		answer: func(context.Context) (*provision.PriceCalculation, error) {
			body := `{"detail":"sku_list[1]: (sku_identifier: cannot be blank.)."}`
			return nil, clierrors.Wrap(clierrors.CodeAPIError, "API returned status 422: "+body, "Cloud.ru rejected the request",
				&provision.StatusError{Status: 422, Body: body})
		}}
	plan := resolvePlan(t, cloud)

	c := plan.Cost
	want := EstimateVM(plan.VM)
	if !strings.HasPrefix(c.Source, "estimate: rate-card") || c.TotalMonthlyRub != want.TotalMonthlyRub || c.Breakdown != nil {
		t.Errorf("cost %+v, want the estimate %+v", c, want)
	}
	if c.PriceAPIError != "HTTP 422: sku_list[1]: (sku_identifier: cannot be blank.)." {
		t.Errorf("price_api_error %q", c.PriceAPIError)
	}
	if c.LicenseNote == "" {
		t.Error("no license note for a marketplace image")
	}
}

// Ответа нет за priceTimeout — оценка по карте, план не ждёт дольше.
func TestPriceVM_TimeoutFallsBack(t *testing.T) {
	old := priceTimeout
	priceTimeout = 50 * time.Millisecond
	t.Cleanup(func() { priceTimeout = old })
	cloud := &priceCloud{answer: func(ctx context.Context) (*provision.PriceCalculation, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	vm := VMSpec{Flavor: FlavorSpec{ID: "fl-1", Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4}, Disk: DiskSpec{SizeGB: 20, Type: "SSD"}}

	start := time.Now()
	c := PriceVM(context.Background(), cloud, PriceInput{ProjectID: "proj-1", VM: vm, PublicIP: true})
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("waited %s", d)
	}
	if c.Source == pricing.SourceAPI || c.TotalMonthlyRub != EstimateVM(vm).TotalMonthlyRub {
		t.Errorf("cost %+v, want the estimate", c)
	}
	if !strings.Contains(c.PriceAPIError, "50ms") {
		t.Errorf("price_api_error %q does not name the timeout", c.PriceAPIError)
	}
	// The disk type is looked up by name for the request.
	if len(cloud.reqs) != 1 || len(cloud.reqs[0].Disks) != 1 || cloud.reqs[0].Disks[0].DiskTypeID != priceDiskType {
		t.Errorf("requests %+v", cloud.reqs)
	}
}

// Без UUID flavor считать нечего: API не спрашивается.
func TestQuoteVM_NoFlavorIDNoCall(t *testing.T) {
	cloud := &priceCloud{answer: probeAnswer}
	_, err := QuoteVM(context.Background(), cloud, PriceInput{ProjectID: "proj-1", VM: VMSpec{Flavor: FlavorSpec{Name: "lowcost10-2-4"}}})
	if err == nil || len(cloud.reqs) != 0 {
		t.Errorf("err %v, requests %v", err, cloud.reqs)
	}
}

func TestAPIMessage(t *testing.T) {
	for body, want := range map[string]string{
		`{"detail":"sku_list[1]: (sku_identifier: cannot be blank.)."}`:        "sku_list[1]: (sku_identifier: cannot be blank.).",
		`{"detail":[{"msg":"field required"}]}`:                                "field required",
		`[{"message":"Image with this id is not found","code":"wrong_image"}]`: "Image with this id is not found",
		`{"message":"bad"}`: "bad",
		"plain\ntext":       "plain text",
	} {
		if got := apiMessage(body); got != want {
			t.Errorf("apiMessage(%s) = %q, want %q", body, got, want)
		}
	}
}

// 200 с нулевым итогом — не цена: оценка по карте с причиной (ревью Fable
// на #80; API отвечает 0 за то, что не умеет считать).
func TestPriceVM_ZeroPriceFallsBack(t *testing.T) {
	cloud := &priceCloud{answer: func(context.Context) (*provision.PriceCalculation, error) {
		return &provision.PriceCalculation{}, nil
	}}
	vm := VMSpec{Flavor: FlavorSpec{ID: "fl-1", Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4}, Disk: DiskSpec{SizeGB: 20, Type: "SSD"}}
	c := PriceVM(context.Background(), cloud, PriceInput{ProjectID: "proj-1", VM: vm})
	if c.Source == pricing.SourceAPI || c.TotalMonthlyRub != EstimateVM(vm).TotalMonthlyRub || !(strings.Contains(c.PriceAPIError, "zero price") || strings.Contains(c.PriceAPIError, "нулевой ценой")) {
		t.Errorf("cost %+v, want the estimate with the reason", c)
	}
}

// Flavor бесплатного уровня без нового floating IP честно стоит 0: это
// цена API, а не «нулевой ценой» с картой (ревью Fable, A4). С новым IP
// ноль по-прежнему не цена.
func TestPriceVM_FreeTierZeroIsThePrice(t *testing.T) {
	cloud := &priceCloud{answer: func(context.Context) (*provision.PriceCalculation, error) {
		return &provision.PriceCalculation{}, nil
	}}
	vm := VMSpec{Flavor: FlavorSpec{ID: "fl-free", Name: "free-tier1", VCPUs: 1, RAMGB: 1, FreeTier: true}, Disk: DiskSpec{SizeGB: 10, Type: "SSD"}}
	c := PriceVM(context.Background(), cloud, PriceInput{ProjectID: "proj-1", VM: vm})
	if c.Source != pricing.SourceAPI || c.TotalMonthlyRub != 0 || c.PriceAPIError != "" {
		t.Errorf("cost %+v, want the API's zero", c)
	}
	c = PriceVM(context.Background(), cloud, PriceInput{ProjectID: "proj-1", VM: vm, PublicIP: true})
	if c.Source == pricing.SourceAPI || c.PriceAPIError == "" {
		t.Errorf("with a new floating IP: cost %+v, want the estimate with the reason", c)
	}
}
