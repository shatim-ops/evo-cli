// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// PriceCalculationRequest is the body of POST
// /api/v1/projects/{project_id}/price-calculation (ProjectPriceRequest of
// the compute spec). The spec forbids other keys; disks and floating_ips,
// when sent, take at least one item. Only total_count is required.
type PriceCalculationRequest struct {
	TotalCount  int               `json:"total_count"`
	FlavorID    string            `json:"flavor_id,omitempty"`
	ImageID     string            `json:"image_id,omitempty"`
	Disks       []PriceDisk       `json:"disks,omitempty"`
	FloatingIPs []PriceFloatingIP `json:"floating_ips,omitempty"`
}

// PriceDisk is one disk of a price calculation: its type by UUID and its
// size in GB.
type PriceDisk struct {
	DiskTypeID string `json:"disk_type_id"`
	Size       int    `json:"size"`
}

// PriceFloatingIP is a number of floating IPs; Action "new" prices
// addresses that are created with the VM.
type PriceFloatingIP struct {
	Action string `json:"floating_ip_action"`
	Count  int    `json:"count"`
}

// PriceCalculation is the answer of price-calculation, in roubles. Keys
// beyond these are ignored, missing ones stay 0. Seen live (api-probe,
// 16.10.2026): image and mkp_image are 0 for every image, marketplace ones
// included — the license of an image is not in the price; two Astra
// marketplace images answer 422 (sku_identifier cannot be blank).
type PriceCalculation struct {
	Flavor          float64 `json:"flavor"`
	Image           float64 `json:"image"`
	MkpImage        float64 `json:"mkp_image"`
	CustomImageSize float64 `json:"custom_image_size"`
	Disks           float64 `json:"disks"`
	FloatingIPs     float64 `json:"floating_ips"`
	NATGateways     float64 `json:"nat_gateways"`
	TotalPriceHour  float64 `json:"total_price_hour"`
	TotalPriceMonth float64 `json:"total_price_month"`
}

// PriceCalculation asks the compute API what the resources of req cost.
// The POST only computes: nothing is created or reserved.
func (a *CloudRuAdapter) PriceCalculation(ctx context.Context, projectID string, req PriceCalculationRequest) (*PriceCalculation, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to marshal price request", "не удалось сформировать запрос расчёта цены"), hintCheckRequest(), err)
	}
	path := fmt.Sprintf("/api/v1/projects/%s/price-calculation", seg(projectID))
	resp, err := a.doWithRetry(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pc PriceCalculation
	if err := json.NewDecoder(resp.Body).Decode(&pc); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to decode price response", "не удалось разобрать ответ расчёта цены"), hintUnexpectedFormat(), err)
	}
	return &pc, nil
}
