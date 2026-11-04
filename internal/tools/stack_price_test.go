// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// The price of a stack (stack_plan's cost) is the price its resources'
// own create previews name (owner's decision 04.11, F10's finding): a
// resource without a price is billable without a price, with why, and no
// sum is named for it — the card of the stack had 5000 ₽ for a k8s
// cluster and 0 ₽ for a secret and a VPC, which no tariff says. The total
// adds the priced resources only, and the note says so.
func TestStackPlan_CostNamesNoSumWithoutAPrice(t *testing.T) {
	stackHome(t)
	// The engine of this world takes no overrides: the VM's image and zone
	// are the server's, as provision's preview reads them.
	r := raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets(),
		sgs: []*provision.SecurityGroup{{ID: "sg-az1", Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: raAZ1,
			Rules: []provision.SGRule{{Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "22:22", RemoteIPPrefix: "0.0.0.0/0"}}}}},
		func(c *config.Config) { c.ImageID, c.Region = raImage, raAZ1 })
	var mf map[string]any
	_ = json.Unmarshal([]byte(`{"name":"s","vpc":{"name":"v"},"k8s":{"name":"k"},
		"postgres":{"name":"p","cpu":2,"ram":8,"storage_gb":20},
		"secrets":[{"path":"a","value":"b"}],
		"vm":{"image":"Ubuntu-24.04","zone":"ru.AZ-1"},
		"nlb":{"name":"l","port":80,"external":true},
		"dns":{"zone_id":"00000000-0000-0000-0000-0000000000d1","name":"www"}}`), &mf)
	m, text, isErr := callNamed(t, r, "stack_plan", map[string]any{"manifest": mf})
	if isErr {
		t.Fatalf("stack_plan: %s", text)
	}
	cost, _ := m["cost"].(map[string]any)
	lines, _ := cost["by_component"].([]any)
	if len(lines) == 0 {
		t.Fatalf("cost without lines: %s", text)
	}
	classes := map[string]string{}
	var sum float64
	unpriced := 0
	for _, l := range lines {
		o := l.(map[string]any)
		res, _ := o["resource"].(string)
		class, _ := o["class"].(string)
		classes[res] = class
		rub, named := o["monthly_rub"].(float64)
		switch engine.PriceClass(class) {
		case engine.PricePriced:
			if !named || rub <= 0 {
				t.Errorf("%v: priced without its sum", o)
			}
			sum += rub
		case engine.PriceZeroAtCreate, engine.PricePaidUnpriced:
			if _, ok := o["monthly_rub"]; ok {
				t.Errorf("%v: a sum for a resource of class %s", o, class)
			}
			if n, _ := o["note"].(string); n == "" {
				t.Errorf("%v: class %s without why", o, class)
			}
			if class == string(engine.PricePaidUnpriced) {
				unpriced++
			}
		default:
			t.Errorf("%v: class %q is none of the three", o, class)
		}
	}
	for _, res := range []string{engine.ResourceK8sCluster, engine.ResourceSecret, engine.ResourceVPC,
		engine.ResourceTargetGroup, engine.ResourceLB, engine.ResourceDNSRecord} {
		if classes[res] != string(engine.PricePaidUnpriced) {
			t.Errorf("%s: class %q, want paid_unpriced: %s", res, classes[res], text)
		}
	}
	// The VM is priced by provision's own preview: the price-calculation API
	// is silent in this world, so by the rate card, with its boot disk and
	// its floating IP.
	for _, res := range []string{engine.ResourceVM, engine.ResourceDisk, engine.ResourceFloatingIP} {
		if classes[res] != string(engine.PricePriced) {
			t.Errorf("%s: class %q, want priced as provision prices it: %s", res, classes[res], text)
		}
	}
	if classes[engine.ResourcePGCluster] != string(engine.PricePriced) {
		t.Errorf("pg_cluster of 2 vCPU and 8 GB: class %q, want priced by its tariff: %s", classes[engine.ResourcePGCluster], text)
	}
	total, _ := cost["total_monthly_rub"].(float64)
	if math.Abs(total-sum) > 0.02 {
		t.Errorf("total %.2f, the priced lines add up to %.2f", total, sum)
	}
	if note, _ := cost["note"].(string); unpriced > 0 && !strings.Contains(note, "paid_unpriced") {
		t.Errorf("note %q: the total leaves out %d billable resource(s) without a price and must say so", note, unpriced)
	}
	if strings.Contains(text, "5000") {
		t.Errorf("the card of the stack is still there: %s", text)
	}
}

// Every step a stack can have names the kind of the resource it creates
// (stackKinds): a step without one would be left out of the price of the
// stack, and its billable resource out of the plan's cost.
func TestStackPlan_EveryStepHasAKind(t *testing.T) {
	var m stack.Manifest
	if err := json.Unmarshal([]byte(`{"name":"s","vpc":{"name":"v"},"postgres":{"name":"p"},"kafka":{"name":"k"},
		"redis":{"name":"r"},"k8s":{"name":"c"},"registry":{"name":"a"},"snat":{"name":"n"},
		"secrets":[{"path":"a","value":"b"}],"s3":[{"bucket":"b","key":"k","content":"c"}],
		"vm":{"name":"vm"},"nlb":{"name":"l","port":80,"external":true},"dns":{"zone_id":"z","name":"www"}}`), &m); err != nil {
		t.Fatal(err)
	}
	j := stack.Plan(m)
	if len(j.Steps) < 13 {
		t.Fatalf("a manifest of every part plans %d steps, want 13 or more", len(j.Steps))
	}
	for _, st := range j.Steps {
		if _, ok := stackKinds[st.Tool]; !ok {
			t.Errorf("step %s (%s): no kind in stackKinds", st.Key, st.Tool)
		}
	}
}
