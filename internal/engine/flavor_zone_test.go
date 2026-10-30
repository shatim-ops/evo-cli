// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// D67 (#597, п. 35–36; agent run C2 on rc.2, Ц-12): a VM of a flavor the
// catalogue names zones for, none of them the zone of the call, is refused
// before the question. A flavor that names no zones, or none it is
// available in, is as before (D54: no decision without the field).
func TestFlavorOutOfZone(t *testing.T) {
	az1 := &deployplan.RefSpec{ID: "00000000-0000-4000-8000-0000000000a1", Name: "ru.AZ-1"}
	byID := &deployplan.RefSpec{ID: az1.ID}
	in := []provision.FlavorZone{{ID: az1.ID, Name: "ru.AZ-1", Enabled: true}, {ID: "z2", Name: "ru.AZ-2", Enabled: true}}
	other := []provision.FlavorZone{{ID: "z2", Name: "ru.AZ-2", Enabled: true}, {ID: az1.ID, Name: "ru.AZ-1", Enabled: false}}
	for _, c := range []struct {
		name  string
		zones []provision.FlavorZone
		zone  *deployplan.RefSpec
		want  bool
	}{
		{"no zones in the answer", nil, az1, false},
		{"zones, none available", []provision.FlavorZone{{ID: "z2", Name: "ru.AZ-2"}}, az1, false},
		{"the zone by name", in, az1, false},
		{"the zone by id", in, byID, false},
		{"the zone by name in another case", in, &deployplan.RefSpec{Name: "RU.AZ-1"}, false},
		{"another zone", other, az1, true},
		{"another zone, by id", other, byID, true},
		{"no zone of the call", other, &deployplan.RefSpec{}, false},
	} {
		f := &provision.Flavor{ID: "f1", Name: "gpa100-40", Zones: c.zones}
		err := FlavorOutOfZone(f, c.zone)
		var ce *clerrors.CLIError
		switch {
		case !c.want && err != nil:
			t.Errorf("%s: refused: %v", c.name, err)
		case c.want && (!errors.As(err, &ce) || ce.Code != clerrors.CodeUsage || !strings.Contains(err.Error(), "ru.AZ-2")):
			t.Errorf("%s: want USAGE naming the flavor's zones, got %v", c.name, err)
		}
	}
}

// D67: deploy without a flavor picks one of the zone of the VM, the same
// in its preview and in the create: the smallest flavor of another zone is
// passed over, not refused.
func TestDeploy_AutoFlavorOfTheZone(t *testing.T) {
	zoned := func(m *mockCloudAPI) {
		m.listFlavorsFn = func(context.Context) ([]*provision.Flavor, error) {
			return []*provision.Flavor{
				{ID: "f-small", Name: "small", VCPUs: 2, RAMGB: 2, Zones: []provision.FlavorZone{{ID: "az-2", Name: "ru-moscow-2", Enabled: true}}},
				{ID: "f-medium", Name: "medium", VCPUs: 4, RAMGB: 4, Zones: []provision.FlavorZone{{ID: "az-1", Name: "ru-moscow-1", Enabled: true}}},
			}, nil
		}
	}
	eng, cloud, opts, stdout, _ := newDeployForConfirm(t)
	zoned(cloud)
	opts.Flavor, opts.DryRun = "", true
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "medium") || strings.Contains(out, "small (") {
		t.Errorf("the preview does not name medium, the flavor of az-1:\n%s", out)
	}

	eng, cloud, opts, _, _ = newDeployForConfirm(t)
	zoned(cloud)
	opts.Flavor, opts.Yes = "", true
	_ = eng.Deploy(context.Background(), opts)
	if !cloud.createCalled || firstNonEmpty(cloud.createReq.FlavorName, cloud.createReq.FlavorID) != "f-medium" && cloud.createReq.FlavorName != "medium" {
		t.Errorf("the create sends flavor %q %q, want medium", cloud.createReq.FlavorName, cloud.createReq.FlavorID)
	}
}

// Fable, FB160-15 and FB160-16: the create checks the flavor against the
// zone the VM goes to — resolved as ResolveZone resolves it, a name in any
// case — before anything is created: a flavor named out of the zone is
// refused with the preview's USAGE, the automatic pick is the zone's.
func TestDeploy_FlavorZoneInTheCreate(t *testing.T) {
	zoned := func(m *mockCloudAPI) {
		m.listFlavorsFn = func(context.Context) ([]*provision.Flavor, error) {
			return []*provision.Flavor{
				{ID: "f-small", Name: "small", VCPUs: 2, RAMGB: 2, Zones: []provision.FlavorZone{{ID: "az-2", Name: "ru-moscow-2", Enabled: true}}},
				{ID: "f-medium", Name: "medium", VCPUs: 4, RAMGB: 4, Zones: []provision.FlavorZone{{ID: "az-1", Name: "ru-moscow-1", Enabled: true}}},
			}, nil
		}
	}
	eng, cloud, opts, _, _ := newDeployForConfirm(t)
	zoned(cloud)
	opts.Region, opts.Flavor, opts.Yes = "RU-MOSCOW-1", "small", true
	err := eng.Deploy(context.Background(), opts)
	var ce *clerrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clerrors.CodeUsage || cloud.createCalled {
		t.Errorf("a flavor of another zone, the zone in upper case: want USAGE and no create, got %v (created %v)", err, cloud.createCalled)
	}

	eng, cloud, opts, _, _ = newDeployForConfirm(t)
	zoned(cloud)
	opts.Region, opts.Flavor, opts.Yes = "RU-MOSCOW-1", "", true
	_ = eng.Deploy(context.Background(), opts)
	if got := firstNonEmpty(cloud.createReq.FlavorName, cloud.createReq.FlavorID); got != "medium" && got != "f-medium" {
		t.Errorf("auto in RU-MOSCOW-1 creates with %q, want medium", got)
	}
}

// Fable, FB160-18: the preview MCP deploy and evo deploy show
// (DeployPreview) names the flavor the create sends, when evo picks it in
// the zone.
func TestDeployPreview_AutoFlavorIsTheCreates(t *testing.T) {
	eng, cloud, opts, _, _ := newDeployForConfirm(t)
	cloud.listFlavorsFn = func(context.Context) ([]*provision.Flavor, error) {
		return []*provision.Flavor{
			{ID: "f-small", Name: "small", VCPUs: 2, RAMGB: 2, Zones: []provision.FlavorZone{{ID: "az-2", Name: "ru-moscow-2", Enabled: true}}},
			{ID: "f-medium", Name: "medium", VCPUs: 4, RAMGB: 4, Zones: []provision.FlavorZone{{ID: "az-1", Name: "ru-moscow-1", Enabled: true}}},
		}, nil
	}
	eng.Config.ComposeFile, eng.Config.Region, eng.Config.Flavor = opts.ComposeFile, "ru-moscow-1", ""
	p, err := eng.DeployPreview(context.Background())
	if err != nil || p == nil || p.VM == nil {
		t.Fatalf("preview: %v %+v", err, p)
	}
	if p.VM.Flavor.Name != "medium" {
		t.Errorf("the preview names %q, want medium, the flavor of ru-moscow-1", p.VM.Flavor.Name)
	}
	opts.Region, opts.Flavor, opts.Yes = "", "", true
	_ = eng.Deploy(context.Background(), opts)
	if got := firstNonEmpty(cloud.createReq.FlavorName, cloud.createReq.FlavorID); got != p.VM.Flavor.Name && got != p.VM.Flavor.ID {
		t.Errorf("the create sends %q, the preview named %q", got, p.VM.Flavor.Name)
	}
}
