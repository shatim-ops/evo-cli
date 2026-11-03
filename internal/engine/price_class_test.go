// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// Release plan 1.7, C (the owner's decision 31.10): every resource a
// preview creates has a class of its price — priced (the preview's cost
// has it), zero_at_create (0 at creation, billed by use, with what the
// tariff bills and its page) or paid_unpriced (billable, its price
// unknown). One line per resource the preview creates, from the list its
// «Will create» line is made of; a kind without a declared class is a
// panic of the one point, not a default.
func TestPriceClasses_EveryKindDeclared(t *testing.T) {
	kinds := append([]string{ResourceFloatingIP}, allResources...)
	if len(priceClasses) != len(kinds) {
		t.Errorf("priceClasses has %d entries, the kinds are %d", len(priceClasses), len(kinds))
	}
	for _, r := range kinds {
		d, ok := priceClasses[r]
		if !ok {
			t.Errorf("%s: no class", r)
			continue
		}
		if n := btoi(d.priceable) + btoi(d.zero != "") + btoi(d.why != ""); n != 1 {
			t.Errorf("%s: declare exactly one of priceable, zero, why: %+v", r, d)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("a kind without a class: want a panic")
		}
	}()
	classOf("no_such_kind", CreatePreview{})
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestPriceClasses_OneLinePerResource(t *testing.T) {
	no := false
	vm := deployplan.VMSpec{Flavor: deployplan.FlavorSpec{Name: "f", VCPUs: 2, RAMGB: 4}, Disk: deployplan.DiskSpec{SizeGB: 20, Type: "SSD"}}
	unsized := deployplan.VMSpec{Disk: deployplan.DiskSpec{SizeGB: 20, Type: "SSD"}}
	for _, c := range []struct {
		name string
		p    CreatePreview
		want []ResourcePrice
	}{
		{"VM with a public address", CreatePreview{Resource: ResourceVM, VM: &vm},
			[]ResourcePrice{{Resource: ResourceVM, Class: PricePriced}, {Resource: ResourceDisk, Class: PricePriced}, {Resource: ResourceFloatingIP, Class: PricePriced}}},
		{"VM without one", CreatePreview{Resource: ResourceVM, VM: &vm, PublicIP: &no},
			[]ResourcePrice{{Resource: ResourceVM, Class: PricePriced}, {Resource: ResourceDisk, Class: PricePriced}}},
		{"VM of an unknown size", CreatePreview{Resource: ResourceVM, VM: &unsized},
			[]ResourcePrice{{Resource: ResourceVM, Class: PricePaidUnpriced}, {Resource: ResourceDisk, Class: PricePaidUnpriced}, {Resource: ResourceFloatingIP, Class: PricePaidUnpriced}}},
		{"subnet", CreatePreview{Resource: ResourceSubnet, Name: "s"}, []ResourcePrice{{Resource: ResourceSubnet, Class: PricePaidUnpriced}}},
	} {
		c.p.Price()
		if !reflect.DeepEqual(c.p.PriceClasses, c.want) {
			t.Errorf("%s: price_classes %+v, want %+v", c.name, c.p.PriceClasses, c.want)
		}
	}

	// A bucket costs nothing at its creation: the tariff bills its storage,
	// operations and traffic. The note says so, with the tariff's page.
	p := CreatePreview{Resource: ResourceS3Bucket, Name: "b"}
	p.Price()
	if len(p.PriceClasses) != 1 || p.PriceClasses[0].Class != PriceZeroAtCreate || p.PriceClasses[0].Note == "" || p.TariffURL == "" {
		t.Fatalf("bucket: %+v, tariff %q", p.PriceClasses, p.TariffURL)
	}
	line := strings.Join(createCostLines(p), "\n")
	if !strings.Contains(line, "0") || !strings.Contains(line, p.PriceClasses[0].Note) || !strings.Contains(line, p.TariffURL) {
		t.Errorf("bucket cost line %q", line)
	}
	b, _ := json.Marshal(p)
	if !strings.Contains(string(b), `"price_classes":[{"resource":"s3_bucket","class":"zero_at_create"`) {
		t.Errorf("JSON: %s", b)
	}
	// A billable resource without a price says it is billable.
	p = CreatePreview{Resource: ResourceSubnet, Name: "s"}
	p.Price()
	if line := strings.Join(createCostLines(p), "\n"); !strings.Contains(line, "billable") || strings.Contains(line, "free") {
		t.Errorf("subnet cost line %q", line)
	}
}
