// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// autoZoneCloud is vmCreateCloud whose smallest lowcost flavor is only in
// ru.AZ-2, and a bigger one in ru.AZ-1, the zone of the call.
type autoZoneCloud struct{ *vmCreateCloud }

const autoZoneFlavor = "00000000-0000-0000-0000-00000000f0a1"

func (autoZoneCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{
		{ID: "00000000-0000-0000-0000-00000000f0a2", Name: "lowcost10-1-2", VCPUs: 1, RAMGB: 2, Share: "1:10", GPU: new(int), Type: &provision.FlavorType{},
			Zones: []provision.FlavorZone{{ID: imgZone2, Name: "ru.AZ-2", Enabled: true}}},
		{ID: autoZoneFlavor, Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4, Share: "1:10", GPU: new(int), Type: &provision.FlavorType{},
			Zones: []provision.FlavorZone{{ID: imgZone1, Name: "ru.AZ-1", Enabled: true}}},
	}, nil
}

// D67: a flavor evo picks itself (no flavor asked) is one of the zone of
// the call: the smallest lowcost of another zone is passed over, not
// refused; the preview names the flavor the create sends (axis 4).
func TestVMCreate_AutoFlavorOfTheZone(t *testing.T) {
	base := withVMCreateCloud(t)
	cloud := autoZoneCloud{base}
	old := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := old(o)
		if err != nil {
			return nil, err
		}
		e.Cloud = cloud
		e.Timings.VMPollInterval = time.Millisecond
		return e, nil
	}
	t.Cleanup(func() { toolEngine = old })

	out, errOut, code := execCLI(t, "--json", "--dry-run", "vm", "create", "web", "--image", "opensearchdashboards", "--meta", "opensearch_host=db.internal", "--no-public-ip")
	if code != 0 || !strings.Contains(out, "lowcost10-2-4") || strings.Contains(out, "lowcost10-1-2") {
		t.Fatalf("preview (exit %d): want lowcost10-2-4, the flavor of ru.AZ-1:\n%s\n%s", code, out, errOut)
	}
	out, errOut, code = execCLI(t, "--json", "--yes", "vm", "create", "web", "--image", "opensearchdashboards", "--meta", "opensearch_host=db.internal", "--no-public-ip")
	if code != 0 || len(base.creates) != 1 {
		t.Fatalf("create (exit %d):\n%s\n%s", code, out, errOut)
	}
	if req := base.creates[0]; req.FlavorName != "lowcost10-2-4" && req.FlavorID != autoZoneFlavor {
		t.Errorf("create sends flavor %q %q, want lowcost10-2-4, as the preview named", req.FlavorName, req.FlavorID)
	}
}
