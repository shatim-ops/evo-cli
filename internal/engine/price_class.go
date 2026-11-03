// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
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

// ResourcePrice is the class of the price of one resource a preview
// creates.
type ResourcePrice struct {
	Resource string     `json:"resource"`
	Class    PriceClass `json:"class"`
	// Note is what the tariff bills by use (zero_at_create).
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
	// why says why evo has no price of a billable resource:
	// paid_unpriced.
	why string
}

// priceClasses are the classes of the kinds of resources previews create
// (TestPriceClasses_EveryKindDeclared keeps the list whole). There is no
// default: a kind without a line is a panic of classOf, so a new kind gets
// a class by a decision, not by falling through.
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
	ResourceDiskResize:  {why: "evo does not price the growth of a disk"},
	ResourceK8sCluster:  {why: "no price of Managed Kubernetes before the calculator (D17)"},
	ResourceK8sNodePool: {why: "no price of Managed Kubernetes before the calculator (D17)"},
	ResourceK8sAddon:    {why: "the addon runs on the cluster's nodes, billed by their tariff"},
	ResourceARRegistry:  {why: "evo has no price of Artifact Registry"},
	ResourceCAApp:       {why: "evo has no price of Container Apps"},
	ResourceKafka:       {why: "no price of Managed Kafka before the calculator (D17)"},
	ResourceLB:          {why: "evo has no price of a load balancer"},
	ResourceRedis:       {why: "no price of Managed Redis before the calculator (D17)"},
	ResourceSNAT:        {why: "evo has no price of a SNAT gateway"},
	ResourceDNSZone:     {why: "evo has no price of Evolution DNS"},
	ResourceS3Object:    {why: "its storage and the upload are billed by the tariff of Object Storage"},
	// No line of the tariffs names these: «count it as billable» rather
	// than a «0» no tariff states (owner's decisions 22.10 and 31.10).
	ResourceImage:       {why: "no line of the tariffs names it"},
	ResourceSecret:      {why: "no line of the tariffs names it"},
	ResourceSubnet:      {why: "no line of the tariffs names it"},
	ResourceVPC:         {why: "no line of the tariffs names it"},
	ResourceVPCRoute:    {why: "no line of the tariffs names it"},
	ResourcePGBackup:    {why: "no line of the tariffs names it"},
	ResourceTargetGroup: {why: "no line of the tariffs names it"},
	ResourceStack:       {why: "each resource of the stack by its own tariff (stack_plan lists them)"},
}

// classOf is the class of the price of resource r of preview p, after its
// Price: a priceable resource is priced when the preview's cost has its own
// share, not only a cost of the whole (a floating IP the estimate left out
// is not priced by the VM's price).
func classOf(r string, p CreatePreview) ResourcePrice {
	d, ok := priceClasses[r]
	if !ok {
		panic("engine: no price class of " + r + ": declare it in priceClasses")
	}
	switch {
	case d.priceable && share(r, p) > 0:
		return ResourcePrice{Resource: r, Class: PricePriced}
	case d.zero != "":
		return ResourcePrice{Resource: r, Class: PriceZeroAtCreate, Note: i18n.T(d.zero, d.zeroRU)}
	}
	return ResourcePrice{Resource: r, Class: PricePaidUnpriced}
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
	switch r {
	case ResourceVM, ResourcePGCluster:
		return c.ComputeMonthlyRub
	case ResourceDisk:
		return c.DiskMonthlyRub
	case ResourceFloatingIP:
		return c.FloatingIPMonthlyRub
	}
	return c.TotalMonthlyRub
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
