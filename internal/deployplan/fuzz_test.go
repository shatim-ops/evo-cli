// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// fuzzCloud is metaCloud that also answers every read Resolve makes for
// any arguments: the project's own images, looked up for an image name the
// catalog does not know.
type fuzzCloud struct{ metaCloud }

func (*fuzzCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, nil
}

// deploy_plan's arguments are the agent's: whatever the name, sizes, names
// of flavor, zone, image and disk type, login and key, Resolve answers a
// plan or an error and does not panic. A plan's id is one Load accepts, and
// a saved plan reads back as the same plan (process audit 1.2.1, second
// wave: deployplan as a fuzz target).
func FuzzResolve(f *testing.F) {
	f.Add("web", "auto", 2, 4, 20, "", "", "", "", "", "ssh-ed25519 AAAA agent")
	f.Add("web", "manual", 0, 0, 0, "lowcost10-2-4", "ru.AZ-2", "Ubuntu-24.04", "SSD", "user1", "")
	f.Add("", "", -1, -1, -1, "nope", "ru.AZ-9", "../x", "?", "root", "not a key")
	f.Add("Имя с пробелом/../..", "auto", 1<<30, 1<<30, 1<<30, "", "", "", "", "", "")
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, name, mode string, cpu, ram, disk int, flavor, zone, image, diskType, login, key string) {
		cloud := &fuzzCloud{metaCloud{fields: ubuntu2404Fields(), cancel: func() {}}}
		p, err := Resolve(context.Background(), cloud, "proj-1", Params{
			Name: name, Mode: mode, CPU: cpu, RAMGB: ram, DiskGB: disk,
			Flavor: flavor, Zone: zone, Image: image, DiskType: diskType, Login: login, SSHKey: key,
		})
		if err != nil {
			return
		}
		if !validPlanID(p.PlanID) {
			t.Fatalf("plan id %q is not one Load accepts", p.PlanID)
		}
		if err := Save(dir, p); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, err := Load(dir, p.PlanID)
		if err != nil || got.VM.Name != p.VM.Name {
			t.Fatalf("Load after Save: %v, VM %q, want %q", err, got.VM.Name, p.VM.Name)
		}
	})
}

// plan_id comes from the MCP client and becomes a file name: an id Load
// accepts never leaves the plans directory.
func FuzzPlanID(f *testing.F) {
	for _, id := range []string{"plan-0123456789ab", "plan-../../x", "../state", "plan-0123456789ab/", `plan-..\..\..\a`, ""} {
		f.Add(id)
	}
	f.Fuzz(func(t *testing.T, id string) {
		if validPlanID(id) && (strings.ContainsAny(id, `/\`) || strings.Contains(id, "..")) {
			t.Fatalf("plan id %q reaches outside the plans directory", id)
		}
	})
}
