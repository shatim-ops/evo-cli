// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"math"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// What a preview knows of the price of each resource it creates (release
// plan 1.7, C; the owner's decision 31.10): an agent reads a price as the
// money the call spends, so «0» is said only where the tariff says so, and
// «unknown» says the resource is billable.

// PriceClass is the class of the price of a resource a preview creates.
type PriceClass string

const (
	// PricePriced: the preview's cost has the resource's price.
	PricePriced PriceClass = "priced"
	// PriceZeroAtCreate: nothing at creation; the tariff bills its use
	// (ResourcePrice.Note says what, tariff_url where).
	PriceZeroAtCreate PriceClass = "zero_at_create"
	// PricePaidUnpriced: billable, evo does not know its price.
	PricePaidUnpriced PriceClass = "paid_unpriced"
)

// ResourceFloatingIP is the floating IP a VM preview creates with the VM:
// a line of its price_classes, not a preview of its own.
const ResourceFloatingIP = "floating_ip"

// ResourceDNSRecord is the DNS record a stack creates in a zone: a line of
// stack_plan's cost, not a preview of its own.
const ResourceDNSRecord = "dns_record"

// ResourcePrice is the class of the price of one resource a preview
// creates.
type ResourcePrice struct {
	Resource string     `json:"resource"`
	Class    PriceClass `json:"class"`
	// Note is what the tariff bills by use (zero_at_create), or why evo
	// has no price of a billable resource (paid_unpriced). Every declared
	// kind has one; a kind priceClasses does not declare has none.
	Note string `json:"note,omitempty"`
}

// priceDecl is how a kind of resource is priced: exactly one is set.
type priceDecl struct {
	// priceable: Price (or the price-calculation API) prices it when the
	// preview knows its size — priced then, paid_unpriced without it.
	priceable bool
	// zero is what the tariff bills by use of a resource that costs
	// nothing at creation, in English and Russian: zero_at_create. Only
	// where the tariff of Cloud.ru says so.
	zero, zeroRU string
	// why says why evo has no price of a billable resource, in English
	// and Russian: paid_unpriced, the note of its line.
	why, whyRU string
}

// priceClasses are the classes of the kinds of resources previews create
// (TestPriceClasses_EveryKindDeclared keeps the list whole: a new kind of
// allResources without a line fails the build, so it gets a class by a
// decision). classOf answers a kind without a line as paid_unpriced, the
// cautious side, rather than fail the CLI in the person's terminal.
var priceClasses = map[string]priceDecl{
	ResourceVM:         {priceable: true},
	ResourceDisk:       {priceable: true},
	ResourceFloatingIP: {priceable: true},
	ResourcePGCluster:  {priceable: true},
	// The tariff of Object Storage bills storage, operations and outgoing
	// traffic, each bucket by its own (docs «Тарификация» of Object
	// Storage): an empty bucket costs nothing.
	ResourceS3Bucket: {zero: "storage, operations and outgoing traffic of the bucket",
		zeroRU: "хранение, операции и исходящий трафик бакета"},
	ResourceDiskResize: {why: "evo does not price the growth of a disk",
		whyRU: "evo не считает цену увеличения диска"},
	ResourceK8sCluster: {why: "no price of Managed Kubernetes before the calculator (D17)",
		whyRU: "цены Managed Kubernetes нет до калькулятора (D17)"},
	ResourceK8sNodePool: {why: "no price of Managed Kubernetes before the calculator (D17)",
		whyRU: "цены Managed Kubernetes нет до калькулятора (D17)"},
	ResourceK8sAddon: {why: "the addon runs on the cluster's nodes, billed by their tariff",
		whyRU: "плагин работает на узлах кластера, они оплачиваются по своему тарифу"},
	ResourceARRegistry: {why: "evo has no price of Artifact Registry",
		whyRU: "у evo нет цены Artifact Registry"},
	ResourceCAApp: {why: "evo has no price of Container Apps",
		whyRU: "у evo нет цены Container Apps"},
	ResourceKafka: {why: "no price of Managed Kafka before the calculator (D17)",
		whyRU: "цены Managed Kafka нет до калькулятора (D17)"},
	ResourceLB: {why: "evo has no price of a load balancer",
		whyRU: "у evo нет цены балансировщика"},
	ResourceRedis: {why: "no price of Managed Redis before the calculator (D17)",
		whyRU: "цены Managed Redis нет до калькулятора (D17)"},
	ResourceSNAT: {why: "evo has no price of a SNAT gateway",
		whyRU: "у evo нет цены шлюза SNAT"},
	ResourceDNSZone: {why: "evo has no price of Evolution DNS",
		whyRU: "у evo нет цены Evolution DNS"},
	ResourceDNSRecord: {why: "evo has no price of Evolution DNS",
		whyRU: "у evo нет цены Evolution DNS"},
	ResourceS3Object: {why: "its storage and the upload are billed by the tariff of Object Storage",
		whyRU: "хранение и загрузка оплачиваются по тарифу Object Storage"},
	// No line of the tariffs names these: «count it as billable» rather
	// than a «0» no tariff states (owner's decisions 22.10 and 31.10).
	ResourceImage: {why: "no line of the tariffs names it",
		whyRU: "в тарифах строки для него нет"},
	ResourceSecret: {why: "no line of the tariffs names it",
		whyRU: "в тарифах строки для него нет"},
	ResourceSubnet: {why: "no line of the tariffs names it",
		whyRU: "в тарифах строки для него нет"},
	ResourceVPC: {why: "no line of the tariffs names it",
		whyRU: "в тарифах строки для него нет"},
	ResourceVPCRoute: {why: "no line of the tariffs names it",
		whyRU: "в тарифах строки для него нет"},
	ResourceBackup: {why: "no line of the tariffs names it",
		whyRU: "в тарифах строки для него нет"},
	ResourcePGBackup: {why: "no line of the tariffs names it",
		whyRU: "в тарифах строки для него нет"},
	ResourceTargetGroup: {why: "no line of the tariffs names it",
		whyRU: "в тарифах строки для него нет"},
	ResourceStack: {why: "each resource of the stack by its own tariff (stack_plan lists them)",
		whyRU: "каждый ресурс стека — по своему тарифу (их перечисляет stack_plan)"},
}

// classOf is the class of the price of resource r of preview p, after its
// Price: a priceable resource is priced when the preview's cost has its own
// share, not only a cost of the whole (a floating IP the estimate left out
// is not priced by the VM's price).
func classOf(r string, p CreatePreview) ResourcePrice {
	d, ok := priceClasses[r]
	if !ok {
		// Not declared: billable without a price, the cautious side, and
		// without a note — the tests of the list and of the previews fail
		// the build for it.
		return ResourcePrice{Resource: r, Class: PricePaidUnpriced}
	}
	switch {
	case d.priceable && share(r, p) > 0:
		return ResourcePrice{Resource: r, Class: PricePriced}
	case d.zero != "":
		return ResourcePrice{Resource: r, Class: PriceZeroAtCreate, Note: i18n.T(d.zero, d.zeroRU)}
	case d.why != "":
		return ResourcePrice{Resource: r, Class: PricePaidUnpriced, Note: i18n.T(d.why, d.whyRU)}
	}
	return ResourcePrice{Resource: r, Class: PricePaidUnpriced, Note: unpricedNote(p)}
}

// unpricedNote is why a priceable resource of preview p has no price: the
// preview has no cost (the size it is priced by is unknown), or its cost
// has no part of it.
func unpricedNote(p CreatePreview) string {
	switch {
	case p.Cost != nil:
		return i18n.T("the cost has no part of it", "в цене нет его доли")
	case p.Resource == ResourceVM:
		return i18n.T("the flavor size is unknown", "размер flavor неизвестен")
	}
	return i18n.T("its size is unknown", "его размер неизвестен")
}

// share is the part of preview p's monthly cost that is resource r's. An
// API price whose parts do not add up to its total (Breakdown "unknown")
// names no parts: its total is the share of the preview's own resource.
func share(r string, p CreatePreview) float64 {
	c := p.Cost
	if c == nil {
		return 0
	}
	if c.Breakdown != nil && c.Breakdown.Period == "unknown" {
		if r == p.Resource {
			return c.TotalMonthlyRub
		}
		return 0
	}
	// A PostgreSQL cluster's storage is part of the cluster, not a disk of
	// its own: its share is the whole price, nodes and storage.
	switch r {
	case ResourceVM:
		return c.ComputeMonthlyRub
	case ResourceDisk:
		return c.DiskMonthlyRub
	case ResourceFloatingIP:
		return c.FloatingIPMonthlyRub
	}
	return c.TotalMonthlyRub
}

// MonthlyRub is the part of preview p's monthly cost that is resource r's,
// when r is priced (its line in p's price_classes); 0 otherwise, which a
// caller does not show as a price.
func (p CreatePreview) MonthlyRub(r string) float64 {
	if classOf(r, p).Class != PricePriced {
		return 0
	}
	return math.Round(share(r, p)*100) / 100
}

// createdResources are the resources preview p creates, in the order its
// «Will create» line names them: a VM brings its boot disk and, with a
// public address, its floating IP.
func createdResources(p CreatePreview) []string {
	if p.Resource != ResourceVM {
		return []string{p.Resource}
	}
	out := []string{ResourceVM}
	if p.VM != nil && p.VM.Disk.SizeGB > 0 {
		out = append(out, ResourceDisk)
	}
	if p.PublicIP == nil || *p.PublicIP {
		out = append(out, ResourceFloatingIP)
	}
	return out
}

// setPriceClasses puts the class of every resource p creates into p.
func (p *CreatePreview) setPriceClasses() {
	p.PriceClasses = nil
	for _, r := range createdResources(*p) {
		p.PriceClasses = append(p.PriceClasses, classOf(r, *p))
	}
}
