// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// priceTimeout bounds the price-calculation call of a plan or a preview: a
// slow answer must not hold the question back, the rate card answers at once.
var priceTimeout = 5 * time.Second

// PriceInput is the VM QuoteVM prices.
type PriceInput struct {
	ProjectID string
	VM        VMSpec
	// PublicIP adds a floating IP created with the VM.
	PublicIP bool
	// Marketplace marks an image of the marketplace: the price has no
	// license in it (LicenseNote says so).
	Marketplace bool
}

// QuoteVM is the price of a VM from the price-calculation API of Cloud.ru,
// asked within priceTimeout. It needs the flavor's UUID; the disk type's
// UUID is looked up by name when the VM has none. The error says, in the
// language of the CLI, why there is no price.
func QuoteVM(ctx context.Context, cloud provision.CloudAPI, in PriceInput) (pricing.Estimate, error) {
	switch {
	case cloud == nil:
		return pricing.Estimate{}, errors.New(i18n.T("no connection to Cloud.ru", "нет подключения к Cloud.ru"))
	case in.ProjectID == "":
		return pricing.Estimate{}, errors.New(i18n.T("project not set", "проект не задан"))
	case in.VM.Flavor.ID == "":
		return pricing.Estimate{}, errors.New(i18n.T("flavor ID unknown", "ID flavor неизвестен"))
	}
	ctx, cancel := context.WithTimeout(ctx, priceTimeout)
	defer cancel()

	req := provision.PriceCalculationRequest{TotalCount: 1, FlavorID: in.VM.Flavor.ID, ImageID: in.VM.Image.ID}
	if d := in.VM.Disk; d.SizeGB > 0 {
		id := d.TypeID
		if id == "" {
			var err error
			if id, err = diskTypeID(ctx, cloud, in.ProjectID, d.Type); err != nil {
				return pricing.Estimate{}, priceError(ctx, err)
			}
		}
		req.Disks = []provision.PriceDisk{{DiskTypeID: id, Size: d.SizeGB}}
	}
	if in.PublicIP {
		req.FloatingIPs = []provision.PriceFloatingIP{{Action: "new", Count: 1}}
	}
	pc, err := cloud.PriceCalculation(ctx, in.ProjectID, req)
	if err != nil {
		return pricing.Estimate{}, priceError(ctx, err)
	}
	// A 200 with a zero total is not a price (the API answers 0 for what
	// it cannot price, e.g. image licences): fall back to the rate card
	// with the reason (ревью Fable на #80). A free-tier flavor without a
	// new floating IP does cost 0: that is the price (ревью Fable, A4).
	if pc.TotalPriceMonth <= 0 && pc.TotalPriceHour <= 0 && !(in.VM.Flavor.FreeTier && !in.PublicIP) {
		return pricing.Estimate{}, fmt.Errorf("%s", i18n.T("the API answered a zero price", "API ответил нулевой ценой"))
	}
	return pricing.FromCalculation(*pc), nil
}

// PriceVM is the price of a VM for a plan or a preview: the API's
// (QuoteVM), else the rate-card estimate (EstimateVM) with the reason in
// PriceAPIError.
func PriceVM(ctx context.Context, cloud provision.CloudAPI, in PriceInput) pricing.Estimate {
	cost, err := QuoteVM(ctx, cloud, in)
	if err != nil {
		cost = EstimateVM(in.VM)
		cost.PriceAPIError = err.Error()
	}
	if in.Marketplace {
		cost.LicenseNote = LicenseNote(cost)
	}
	return cost
}

// LicenseNote says that the price of a VM from a marketplace image has no
// license in it: the API answers 0 for every image (seen live 16.10.2026),
// the rate card has no images.
func LicenseNote(cost pricing.Estimate) string {
	if cost.Source == pricing.SourceAPI {
		return i18n.T("marketplace image: its license is not in the price (the price-calculation API answers 0 for it)",
			"образ маркетплейса: лицензии в цене нет (API расчёта цены отвечает за образ 0)")
	}
	return i18n.T("marketplace image: its license is not in the price (the rate card has no images)",
		"образ маркетплейса: лицензии в цене нет (в тарифной карте образов нет)")
}

// diskTypeID is the UUID of the project's disk type by name (SSD when
// none is given), as the create call matches it.
func diskTypeID(ctx context.Context, cloud provision.CloudAPI, projectID, name string) (string, error) {
	name = firstNonEmpty(name, "SSD")
	dts, err := cloud.ListDiskTypes(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, dt := range dts {
		if strings.EqualFold(dt.Name, name) && dt.ID != "" {
			return dt.ID, nil
		}
	}
	return "", errors.New(i18n.Tf("disk type %s not found in the project", "тип диска %s в проекте не найден", name))
}

// priceError is the reason of a failed price call, short enough for a line
// of the preview: no answer in time, the HTTP status with the API's message
// (422 sku_identifier for an image without a SKU in billing), or the error.
func priceError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New(i18n.Tf("no answer within %s", "нет ответа за %s", priceTimeout))
	}
	var se *provision.StatusError
	if errors.As(err, &se) {
		return fmt.Errorf("HTTP %d: %s", se.Status, apiMessage(se.Body))
	}
	return errors.New(clip(err.Error()))
}

// apiMessage is the message of an error body of the compute API: detail
// (a string or a list of {msg}), message, or [{message}]; else the body.
func apiMessage(body string) string {
	var obj struct {
		Detail  json.RawMessage `json:"detail"`
		Message string          `json:"message"`
	}
	if json.Unmarshal([]byte(body), &obj) == nil {
		var s string
		if json.Unmarshal(obj.Detail, &s) == nil && s != "" {
			return clip(s)
		}
		var items []struct {
			Msg string `json:"msg"`
		}
		if json.Unmarshal(obj.Detail, &items) == nil && len(items) > 0 && items[0].Msg != "" {
			return clip(items[0].Msg)
		}
		if obj.Message != "" {
			return clip(obj.Message)
		}
	}
	var list []struct {
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &list) == nil && len(list) > 0 && list[0].Message != "" {
		return clip(list[0].Message)
	}
	return clip(body)
}

// clip keeps a message to one short line.
func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}
