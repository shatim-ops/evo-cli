// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A catalog shaped like the real one: marketplace and GPU builds around the
// plain distribution images, empty os/status fields.
func catalogCloud() *mockCloudAPI {
	return &mockCloudAPI{
		listAvailabilityZonesFn: func(context.Context) ([]*provision.AvailabilityZone, error) {
			return []*provision.AvailabilityZone{{ID: "az-1-id", Name: "ru.AZ-1"}, {ID: "az-2-id", Name: "ru.AZ-2"}}, nil
		},
		listSubnetsFn: func(context.Context, string) ([]*provision.Subnet, error) {
			return []*provision.Subnet{
				{ID: "sn-1", Name: "Default_ru.AZ-1", AvailabilityZoneID: "az-1-id", Default: true},
				{ID: "sn-2", Name: "app", AvailabilityZoneID: "az-2-id"},
			}, nil
		},
		listImagesFn: func(context.Context) ([]*provision.Image, error) {
			return []*provision.Image{
				{ID: "talos", Name: "Talos_Linux"},
				{ID: "gpu", Name: "NVLINK-Ubuntu-24.04-gpu"},
				{ID: "desk", Name: "Ubuntu_24_04_Desktop"},
				{ID: "deb11err", Name: "debian-11_error"},
				{ID: "deb12", Name: "debian-12"},
				{ID: "docker", Name: "Docker"},
				{ID: "u2204", Name: "ubuntu-22.04"},
				{ID: "u2404", Name: "Ubuntu-24.04"},
				{ID: "u2004", Name: "ubuntu-20.04"},
				{ID: "nginx", Name: "nginx-ubuntu-22.04"},
			}, nil
		},
	}
}

func TestDeployableImages_PlainUbuntuNewestFirstThenDebian(t *testing.T) {
	imgs, _ := catalogCloud().ListImages(context.Background())
	var got []string
	for _, img := range deployableImages(imgs) {
		got = append(got, img.ID)
	}
	if strings.Join(got, ",") != "u2404,u2204,u2004,deb12" {
		t.Errorf("order = %v", got)
	}
}

func initEngine(t *testing.T) (*Engine, string, *bytes.Buffer) {
	t.Helper()
	eng, dir := newTestEngine(t, catalogCloud(), newMockDeployer())
	var out bytes.Buffer
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)
	return eng, filepath.Join(dir, ".evo.yaml"), &out
}

// --non-interactive: the zone with the project's main subnet (no zone of
// the list is marked default), the newest plain Ubuntu, choice printed; no
// UUID is baked into the binary.
func TestInit_NonInteractiveTakesDefaultZoneAndImage(t *testing.T) {
	withPromptStdin(t, strings.NewReader("2\n"), true) // a terminal, but the flag wins
	eng, path, out := initEngine(t)

	if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(data)
	for _, want := range []string{"region: az-1-id  # ru.AZ-1", "image_id: u2404  # Ubuntu-24.04",
		"project_id: 00000000-0000-0000-0000-000000000001"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %q:\n%s", want, cfg)
		}
	}
	for _, want := range []string{"Zone: ru.AZ-1 (az-1-id)", "Image: Ubuntu-24.04 (u2404)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("choice not printed (%q):\n%s", want, out.String())
		}
	}
}

// The zone of init is the platform's own default (the zone list marks it)
// when it takes new resources and the project has a subnet there; else
// the zone of the project's main subnet; else any zone with a subnet of
// the project — deploy cannot plug a VM in elsewhere. One idea of «the
// default zone» with evo vm zones, and none from one day's runs (agent
// run C2, Ц-7; the release's choice 03.11). A new project has a subnet
// only in ru.AZ-1 (docs, «Подсети»: Default_ru.AZ-1).
func TestInit_ZoneWithSubnet(t *testing.T) {
	closed := false
	both := []*provision.Subnet{{ID: "sn-1", Name: "Default_ru.AZ-1", AvailabilityZoneID: "az-1-id", Default: true},
		{ID: "sn-2", Name: "app", AvailabilityZoneID: "az-2-id"}}
	for _, tc := range []struct {
		name    string
		zones   []*provision.AvailabilityZone
		subnets []*provision.Subnet
		want    string
		say     string
	}{
		{"подсеть только в ru.AZ-1", nil, []*provision.Subnet{both[0]}, "region: az-1-id", ""},
		{"подсети в обеих зонах — зона основной подсети", nil, nil, "region: az-1-id", ""},
		{"подсетей нет — первая зона и предупреждение", nil, []*provision.Subnet{}, "region: az-1-id", "no subnet in any zone"},
		{"зона по умолчанию платформы — она", []*provision.AvailabilityZone{{ID: "az-1-id", Name: "ru.AZ-1"}, {ID: "az-2-id", Name: "ru.AZ-2", Default: true}},
			both, "region: az-2-id", ""},
		{"зона по умолчанию закрыта — следующая включённая", []*provision.AvailabilityZone{{ID: "az-1-id", Name: "ru.AZ-1"}, {ID: "az-2-id", Name: "ru.AZ-2", Default: true, Enabled: &closed}},
			both, "region: az-1-id", "zone ru.AZ-2 is closed"},
		{"зона по умолчанию без подсети проекта — с подсетью, и почему", []*provision.AvailabilityZone{{ID: "az-1-id", Name: "ru.AZ-1"}, {ID: "az-2-id", Name: "ru.AZ-2", Default: true}},
			[]*provision.Subnet{both[0]}, "region: az-1-id", "ru.AZ-2 (the default zone of the zone list) is not offered first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, path, out := initEngine(t)
			if tc.zones != nil {
				zs := tc.zones
				eng.Cloud.(*mockCloudAPI).listAvailabilityZonesFn = func(context.Context) ([]*provision.AvailabilityZone, error) { return zs, nil }
			}
			if tc.subnets != nil {
				sn := tc.subnets
				eng.Cloud.(*mockCloudAPI).listSubnetsFn = func(context.Context, string) ([]*provision.Subnet, error) { return sn, nil }
			}
			if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(path)
			if !strings.Contains(string(data), tc.want) {
				t.Errorf("config lacks %q:\n%s", tc.want, data)
			}
			if tc.say != "" && !strings.Contains(out.String(), tc.say) {
				t.Errorf("output lacks %q:\n%s", tc.say, out.String())
			}
			if tc.say == "" && (strings.Contains(out.String(), "not offered first") || strings.Contains(out.String(), "no subnet")) {
				t.Errorf("unexpected note:\n%s", out.String())
			}
		})
	}
}

// Зона с enabled=false не предлагается, даже если подсеть есть только в
// ней (ru.AZ-1, 17.10.2026): deploy туда получил бы 422 wrong_az.
func TestInit_ClosedZoneNotOffered(t *testing.T) {
	eng, path, out := initEngine(t)
	closed := false
	eng.Cloud.(*mockCloudAPI).listAvailabilityZonesFn = func(context.Context) ([]*provision.AvailabilityZone, error) {
		return []*provision.AvailabilityZone{{ID: "az-1-id", Name: "ru.AZ-1", Enabled: &closed}, {ID: "az-2-id", Name: "ru.AZ-2"}}, nil
	}
	eng.Cloud.(*mockCloudAPI).listSubnetsFn = func(context.Context, string) ([]*provision.Subnet, error) {
		return []*provision.Subnet{{ID: "sn-1", Name: "Default_ru.AZ-1", AvailabilityZoneID: "az-1-id", Default: true}}, nil
	}
	if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "region: az-2-id") {
		t.Errorf("config:\n%s", data)
	}
	if !strings.Contains(out.String(), "zone ru.AZ-1 is closed") {
		t.Errorf("output:\n%s", out.String())
	}
}

// Without a terminal init does not ask either.
func TestInit_NoTTYDoesNotAsk(t *testing.T) {
	withPromptStdin(t, strings.NewReader(""), false)
	eng, path, out := initEngine(t)
	if err := eng.Init(context.Background(), InitOptions{Path: path}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if strings.Contains(out.String(), "Choose") {
		t.Errorf("asked without a terminal:\n%s", out.String())
	}
}

func TestInit_InteractiveChoice(t *testing.T) {
	withPromptStdin(t, strings.NewReader("2\n"), true)
	eng, path, out := initEngine(t)
	// Each prompt reads one line; the second gets EOF → default.
	if err := eng.Init(context.Background(), InitOptions{Path: path}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, _ := os.ReadFile(path)
	// ru.AZ-1, the zone of the main subnet, is offered first, so "2" picks
	// ru.AZ-2.
	if !strings.Contains(string(data), "region: az-2-id") {
		t.Errorf("zone choice ignored:\n%s", data)
	}
	if !strings.Contains(out.String(), "1) ru.AZ-1") || !strings.Contains(out.String(), "2) ru.AZ-2") {
		t.Errorf("options not listed:\n%s", out.String())
	}
}

func TestInit_InvalidChoice(t *testing.T) {
	withPromptStdin(t, strings.NewReader("7\n"), true)
	eng, path, _ := initEngine(t)
	if err := eng.Init(context.Background(), InitOptions{Path: path}); err == nil {
		t.Fatal("want an error for an out-of-range choice")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("config written despite the invalid choice")
	}
}

// init warns, but does not fail, when the compose file deploy reads is
// missing: otherwise one learns about it only at 'evo deploy'.
func TestInit_WarnsWithoutComposeFile(t *testing.T) {
	eng, path, out := initEngine(t)
	compose := filepath.Join(filepath.Dir(path), "docker-compose.yml")
	eng.Config.ComposeFile = compose
	if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !strings.Contains(out.String(), "Warning: "+compose+" not found") {
		t.Errorf("no warning about the missing compose file:\n%s", out.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config not written: %v", err)
	}

	eng, path, out = initEngine(t)
	compose = filepath.Join(filepath.Dir(path), "docker-compose.yml")
	eng.Config.ComposeFile = compose
	if err := os.WriteFile(compose, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if strings.Contains(out.String(), "not found") {
		t.Errorf("warning with the compose file present:\n%s", out.String())
	}
}

func TestInit_ExistingConfigUntouched(t *testing.T) {
	eng, path, _ := initEngine(t)
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng.Cloud = nil // must not reach the API
	if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Errorf("config overwritten: %q", data)
	}
}

// evo init writes project_name: the VM of deploy is evo-<project_name>, and
// the person (and the agent, by the skill evo-deploy) sees the name in
// .evo.yaml instead of guessing it from the directory (agent run C2, Ц-6).
// The name is the directory's, as deploy takes without it, so a deploy
// after init names the same VM as before.
func TestInit_WritesProjectName(t *testing.T) {
	eng, path, _ := initEngine(t)
	if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Read back as YAML: a directory named like a number ("001") is
	// quoted, so the name stays a string.
	var cfg struct {
		ProjectName string `yaml:"project_name"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("config is not YAML: %v\n%s", err, data)
	}
	if want := filepath.Base(filepath.Dir(path)); cfg.ProjectName != want {
		t.Errorf("project_name %q, want the directory's %q:\n%s", cfg.ProjectName, want, data)
	}
}

// A name YAML would read as another type or structure is quoted, so it
// reads back as the same string, on one line; a plain one is not quoted.
func TestYAMLScalar_KeepsAString(t *testing.T) {
	for _, in := range []string{"shop", "001", "yes", "a: b", "my-app_2", "#tag", "x\ny"} {
		out := yamlScalar(in)
		var back string
		if err := yaml.Unmarshal([]byte("k: "+out), &struct {
			K *string `yaml:"k"`
		}{&back}); err != nil || strings.Contains(out, "\n") || back != strings.ReplaceAll(in, "\n", " ") {
			t.Errorf("yamlScalar(%q) = %s: reads back %q (%v)", in, out, back, err)
		}
	}
	if got := yamlScalar("shop"); got != "shop" {
		t.Errorf("yamlScalar(shop) = %s, want it plain", got)
	}
}

// Without ru.AZ-2 the API order stays: the first zone is the default.
func TestPreferredZoneFirst(t *testing.T) {
	names := func(zs []*provision.AvailabilityZone) string {
		var n []string
		for _, z := range zs {
			n = append(n, z.Name)
		}
		return strings.Join(n, ",")
	}
	in := []*provision.AvailabilityZone{{Name: "ru.AZ-1"}, {Name: "ru.AZ-3"}, {Name: "ru.AZ-2"}}
	if got := names(PreferredZoneFirst(in)); got != "ru.AZ-2,ru.AZ-1,ru.AZ-3" {
		t.Errorf("order = %s", got)
	}
	if got := names(in); got != "ru.AZ-1,ru.AZ-3,ru.AZ-2" {
		t.Errorf("input changed: %s", got)
	}
	other := []*provision.AvailabilityZone{{Name: "kz.AZ-1"}, {Name: "kz.AZ-2"}}
	if got := names(PreferredZoneFirst(other)); got != "kz.AZ-1,kz.AZ-2" {
		t.Errorf("order without ru.AZ-2 = %s", got)
	}
}

// The zone list's default goes first while it takes new resources; a
// closed default is no default; without one the API's order stays; the
// input is not changed (agent run C2, Ц-7).
func TestDefaultZoneFirst(t *testing.T) {
	names := func(zs []*provision.AvailabilityZone) string {
		var n []string
		for _, z := range zs {
			n = append(n, z.Name)
		}
		return strings.Join(n, ",")
	}
	closed := false
	for _, tc := range []struct {
		in   []*provision.AvailabilityZone
		want string
	}{
		{[]*provision.AvailabilityZone{{Name: "ru.AZ-1"}, {Name: "ru.AZ-2"}, {Name: "ru.AZ-3", Default: true}}, "ru.AZ-3,ru.AZ-1,ru.AZ-2"},
		{[]*provision.AvailabilityZone{{Name: "ru.AZ-1", Default: true, Enabled: &closed}, {Name: "ru.AZ-2"}}, "ru.AZ-1,ru.AZ-2"},
		{[]*provision.AvailabilityZone{{Name: "ru.AZ-2"}, {Name: "ru.AZ-1"}}, "ru.AZ-2,ru.AZ-1"},
	} {
		before := names(tc.in)
		if got := names(DefaultZoneFirst(tc.in)); got != tc.want {
			t.Errorf("DefaultZoneFirst(%s) = %s, want %s", before, got, tc.want)
		}
		if names(tc.in) != before {
			t.Errorf("input changed: %s", names(tc.in))
		}
	}
}
