// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/pricing"
)

// Ц-10 (owner's decision 29.10, #570, «b»): the preview of pg_create
// prices the cluster before consent by the tariff of Managed PostgreSQL,
// says the price is an estimate by the tariff of its date and links the
// tariff. A cluster whose size is not known goes unpriced, with the link.
func TestPGCreatePreview_PricedByTheTariff(t *testing.T) {
	p := pgCreatePreview(config.Config{}, map[string]any{"name": "db", "cpu": float64(2), "memory_gb": float64(8),
		"storage_gb": 50, "instances": 2})
	p.Price()
	want := pricing.EstimatePG(pricing.PGSize{CPU: 2, RAMGB: 8, StorageGB: 50, Instances: 2})
	if p.Cost == nil || *p.Cost != want {
		t.Fatalf("cost %+v, want %+v", p.Cost, want)
	}
	if !strings.Contains(p.CostNote, pricing.PGTariffDate) || p.TariffURL != engine.TariffURL(engine.ResourcePGCluster) {
		t.Errorf("note %q, tariff %q: the estimate names neither the tariff's date nor its page", p.CostNote, p.TariffURL)
	}
	bare := pgCreatePreview(config.Config{}, map[string]any{"name": "db"})
	bare.Price()
	if bare.Cost != nil || bare.TariffURL == "" {
		t.Errorf("a cluster of no known size: cost %+v, tariff %q", bare.Cost, bare.TariffURL)
	}
}
