package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Релизный аудит, A-002: deploy_plan берёт зону, флейвор и образ из настроек
// (профиль, .evo.yaml, CLOUDRU_*), как evo deploy; аргументы вызова важнее.
// Раньше план всегда шёл в ru.AZ-2, а deploy — в настроенную зону. ID
// выдуманы.

const (
	planZoneAZ1 = "00000000-0000-0000-0000-0000000000a1"
	planZoneAZ2 = "00000000-0000-0000-0000-0000000000a2"
)

// planCloud отвечает на чтения deployplan.Resolve; ничего не создаёт.
type planCloud struct{ provision.CloudAPI }

func (planCloud) SetToken(string) {}
func (planCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: planZoneAZ1, Name: "ru.AZ-1"}, {ID: planZoneAZ2, Name: "ru.AZ-2"}}, nil
}
func (planCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{
		{ID: "fl-small", Name: "lowcost10-1-1", VCPUs: 1, RAMGB: 1},
		{ID: "fl-big", Name: "gen-4-8", VCPUs: 4, RAMGB: 8},
	}, nil
}
func (planCloud) ListImages(_ context.Context, f ...provision.ImageFilter) ([]*provision.Image, error) {
	return []*provision.Image{{ID: "img-u", Name: "Ubuntu-24.04"}, {ID: "img-d", Name: "Debian-12"}}, nil
}
func (planCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, nil
}
func (planCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	name := map[string]string{"img-u": "Ubuntu-24.04", "img-d": "Debian-12"}[id]
	return &provision.ImageDetail{ID: id, Name: name}, nil
}
func (planCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return nil, nil
}
func (planCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{
		{ID: "sn-1", Name: "Default_ru.AZ-1", AvailabilityZoneID: planZoneAZ1},
		{ID: "sn-2", Name: "Default_ru.AZ-2", AvailabilityZoneID: planZoneAZ2},
	}, nil
}
func (planCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{
		{ID: "sg-1", Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: planZoneAZ1},
		{ID: "sg-2", Name: "SSH-access_ru.AZ-2", AvailabilityZoneID: planZoneAZ2},
	}, nil
}
func (planCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, nil
}

func planRegistry(t *testing.T, cfg config.Config) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	cfg.ProjectID, cfg.KeyID, cfg.Secret = "proj-1", "k", "s"
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    cfg,
			Auth:      staticToken{},
			Cloud:     planCloud{},
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
		}, nil
	}), dir
}

func TestDeployPlan_DefaultsFromConfig(t *testing.T) {
	configured := config.Config{Region: planZoneAZ1, Flavor: "gen-4-8", ImageID: "img-d"}
	for name, tc := range map[string]struct {
		cfg                 config.Config
		args                map[string]any
		zone, flavor, image string
		wantSourceZone      string
		wantZoneWarning     bool
	}{
		"из настроек": {configured, map[string]any{"name": "web"},
			"ru.AZ-1", "gen-4-8", "Debian-12", "", false},
		"аргументы важнее": {configured, map[string]any{"name": "web", "zone": "ru.AZ-2", "flavor": "lowcost10-1-1", "image": "Ubuntu-24.04"},
			"ru.AZ-2", "lowcost10-1-1", "Ubuntu-24.04", "ru.AZ-2", false},
		// cpu/ram — подбор: флейвор из настроек его не перебивает.
		"cpu и ram — подбор": {configured, map[string]any{"name": "web", "cpu": 1, "ram_gb": 1},
			"ru.AZ-1", "lowcost10-1-1", "Debian-12", "", false},
		// Ничего не настроено: ru.AZ-2, и план об этом предупреждает.
		"без настроек": {config.Config{Flavor: "auto"}, map[string]any{"name": "web"},
			"ru.AZ-2", "lowcost10-1-1", "Ubuntu-24.04", "", true},
	} {
		t.Run(name, func(t *testing.T) {
			r, dir := planRegistry(t, tc.cfg)
			text, isErr := r.handlers["deploy_plan"](context.Background(), tc.args)
			if isErr {
				t.Fatalf("deploy_plan: %s", text)
			}
			var view struct {
				PlanID   string            `json:"plan_id"`
				VM       deployplan.VMSpec `json:"vm"`
				Warnings []string          `json:"warnings"`
			}
			if err := json.Unmarshal([]byte(text), &view); err != nil {
				t.Fatal(err)
			}
			if view.VM.Zone.Name != tc.zone || view.VM.Flavor.Name != tc.flavor || view.VM.Image.Name != tc.image {
				t.Errorf("план: зона %q, флейвор %q, образ %q; ждали %q, %q, %q",
					view.VM.Zone.Name, view.VM.Flavor.Name, view.VM.Image.Name, tc.zone, tc.flavor, tc.image)
			}
			warned := false
			for _, w := range view.Warnings {
				if w == "no zone given and no region configured: planning in ru.AZ-2 (pass zone, or set region in the profile or .evo.yaml)" {
					warned = true
				}
			}
			if warned != tc.wantZoneWarning {
				t.Errorf("предупреждение о ru.AZ-2: %v, ждали %v (%v)", warned, tc.wantZoneWarning, view.Warnings)
			}
			// Патч поверх плана исходит из запроса, а не из подставленных
			// значений: в source_params — только то, что передали.
			plan, err := deployplan.Load(dir, view.PlanID)
			if err != nil {
				t.Fatal(err)
			}
			if plan.SourceParams.Zone != tc.wantSourceZone {
				t.Errorf("source_params.zone = %q, ждали %q", plan.SourceParams.Zone, tc.wantSourceZone)
			}
		})
	}
}
