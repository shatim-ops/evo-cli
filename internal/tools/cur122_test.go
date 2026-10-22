// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The findings of the adversarial review of 1.2.2 (Cursor, brief 4,
// docs/internal/review-1.2.2-cursor.md, CUR122-2…5), as the tests that
// failed on 80511fd. CUR122-1 is TestStackPlan_S3BucketNameCheckedAtPlan.

func TestCUR122_BucketCreatePreviewForNameS3Rejects(t *testing.T) {
	r := newTestServer()
	for _, name := range []string{"a..b", "192.168.0.1"} {
		m, text, isErr := callNamed(t, r, "s3_bucket_create", map[string]any{"bucket": name})
		if !isErr || m["reason"] != "USAGE" || m["status"] == "confirmation_required" {
			t.Errorf("%s: want USAGE before consent, got %s", name, text)
		}
	}
}

func TestCUR122_DeployPlanZoneNameIgnoresCase(t *testing.T) {
	r, _ := planRegistry(t, config.Config{ImageID: "img-u"})
	m, text, isErr := callNamed(t, r, "deploy_plan", map[string]any{"name": "web", "zone": "ru.az-2"})
	if isErr {
		t.Fatalf("ru.az-2 is ru.AZ-2: %s", text)
	}
	vm, _ := m["vm"].(map[string]any)
	zone, _ := vm["zone"].(map[string]any)
	if zone["id"] != planZoneAZ2 || zone["name"] != "ru.AZ-2" {
		t.Fatalf("zone %+v in %s", zone, text)
	}
}

func TestCUR122_EmptyZoneListNameReachesAPI(t *testing.T) {
	cloud := &fipZoneCloud{otherZoneCloud: otherZoneCloud{
		cancelCloud: cancelCloud{cancel: func() {}},
		zones:       []*provision.AvailabilityZone{},
		subnets:     []*provision.Subnet{{ID: "sn-1", Name: "default"}},
		groups:      []*provision.SecurityGroup{{ID: "sg-1", Name: "default", Default: true}},
	}}
	r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", ImageID: "img-1", KeyID: "k", Secret: "s"})
	// An empty list names no zone: the name is refused before the
	// preview, with confirmed or without, and nothing is created.
	for _, confirmed := range []bool{false, true} {
		m, text, isErr := callNamed(t, r, "provision", map[string]any{"name": "web", "region": "ru.AZ-2", "confirmed": confirmed})
		if !isErr || m["reason"] != "USAGE" || m["status"] == "confirmation_required" {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
	}
	if cloud.fipZone != "" || cloud.createReq.AvailabilityZoneID != "" || len(cloud.mutations) > 0 {
		t.Fatalf("name reached the API (fip zone %q, vm zone %q, mutations %v)",
			cloud.fipZone, cloud.createReq.AvailabilityZoneID, cloud.mutations)
	}
}

type fipZoneCloud struct {
	otherZoneCloud
	fipZone string
}

func (c *fipZoneCloud) CreateFloatingIP(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipZone = req.AvailabilityZoneID
	c.mutations = append(c.mutations, "CreateFloatingIP")
	return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.9", State: "available"}, nil
}

func (c *fipZoneCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	c.createReq = req
	c.mutations = append(c.mutations, "CreateVM")
	return nil, errors.New("stop after the zone was sent")
}

func TestCUR122_K8sRussianHintsNameCommands(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	r := newTestServer()
	m, text, isErr := callNamed(t, r, "k8s_nodepools", nil)
	hint, _ := m["hint"].(string)
	if !isErr || strings.Contains(hint, "k8s_") || strings.Contains(hint, "confirmed=true") {
		t.Errorf("nodepools hint: %s", text)
	}
	cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}}
	r = zoneRegistry(t, cloud, config.Config{ProjectID: "p", KeyID: "k", Secret: "s"})
	pc := &mk8s.ProductConfig{}
	pc.Zones = append(pc.Zones, struct {
		ZoneID           string   `json:"zoneId"`
		Name             string   `json:"name"`
		AvailableFlavors []string `json:"availableFlavors"`
	}{ZoneID: "z", Name: "ru.AZ-3"})
	_, err := r.k8sZone(context.Background(), pc, "no-such-zone")
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || strings.Contains(ce.Hint, "k8s_") || strings.Contains(ce.Hint, "confirmed=true") {
		t.Fatalf("zone hint: %v", err)
	}
}
