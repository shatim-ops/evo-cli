// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// provision with an image the catalogue does not have showed the preview
// with the price and asked for consent; the refusal came after «yes»
// (Grok live run on 1.3.0, G130-02, 3 of 3 on CLI and MCP). The
// description promises NOT_FOUND before anything is created. The image is
// resolved in the plan (D35, D39): no such image — NOT_FOUND, several —
// USAGE with the candidates, with confirmed or without, and the cloud is
// not touched. The IDs are made up.

// imageShelfCloud is a cloud with a shelf of images; any mutation counts.
type imageShelfCloud struct {
	cancelCloud
	images    []*provision.Image
	mutations []string
}

func (c *imageShelfCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return c.images, nil
}
func (c *imageShelfCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	for _, img := range c.images {
		if img.ID == id {
			return &provision.ImageDetail{ID: img.ID, Name: img.Name}, nil
		}
	}
	return nil, clierrors.New(clierrors.CodeNotFound, "HTTP 404: image not found", "")
}

// The reads of the preview cancelCloud has not (as productCloud).
func (*imageShelfCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, errNoPriceAPI
}
func (*imageShelfCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return nil, errNoPriceAPI
}
func (c *imageShelfCloud) CreateFloatingIP(ctx context.Context, r provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.mutations = append(c.mutations, "CreateFloatingIP")
	return c.cancelCloud.CreateFloatingIP(ctx, r)
}
func (c *imageShelfCloud) CreateVM(ctx context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.mutations = append(c.mutations, "CreateVM")
	return c.cancelCloud.CreateVM(ctx, req)
}

const (
	catalogUbuntu22 = "00000000-0000-4000-8000-000000000a22"
	catalogUbuntu24 = "00000000-0000-4000-8000-000000000a24"
	catalogNoSuchID = "00000000-0000-4000-8000-000000000bad"
)

func TestProvision_UnknownImageRefusedBeforePreview(t *testing.T) {
	images := []*provision.Image{
		{ID: catalogUbuntu22, Name: "Ubuntu-22.04", DisplayName: "Ubuntu 22.04", Status: "active"},
		{ID: catalogUbuntu24, Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Status: "active"},
	}
	for name, tc := range map[string]struct {
		args       map[string]any
		reason     string
		candidates bool
	}{
		"image by name, no such":   {map[string]any{"image": "NoSuch-1.0"}, clierrors.CodeNotFound, false},
		"image_id, no such":        {map[string]any{"image_id": catalogNoSuchID}, clierrors.CodeNotFound, false},
		"image by UUID, no such":   {map[string]any{"image": catalogNoSuchID}, clierrors.CodeNotFound, false},
		"image by name, ambiguous": {map[string]any{"image": "Ubuntu"}, clierrors.CodeUsage, true},
	} {
		for _, confirmed := range []bool{false, true} {
			t.Run(name+"/confirmed="+boolText(confirmed), func(t *testing.T) {
				cloud := &imageShelfCloud{cancelCloud: cancelCloud{cancel: func() {}}, images: images}
				r, _ := cancelRegistry(t, cloud)
				args := map[string]any{"name": "web", "confirmed": confirmed}
				for k, v := range tc.args {
					args[k] = v
				}
				text, isErr := r.handlers["provision"](context.Background(), args)
				var m map[string]any
				if err := json.Unmarshal([]byte(text), &m); err != nil {
					t.Fatalf("answer is not JSON: %v: %s", err, text)
				}
				if !isErr || m["reason"] != tc.reason {
					t.Errorf("isErr=%v reason=%v, want %s before the question: %s", isErr, m["reason"], tc.reason, text)
				}
				if strings.Contains(text, "confirmation_required") || strings.Contains(text, "monthly") {
					t.Errorf("a preview with a price for an image the catalogue does not have: %s", text)
				}
				if _, ok := m["candidates"]; ok != tc.candidates {
					t.Errorf("candidates present=%v, want %v: %s", ok, tc.candidates, text)
				}
				if len(cloud.mutations) > 0 || cloud.creates > 0 {
					t.Errorf("the cloud was changed before the refusal: %v", cloud.mutations)
				}
			})
		}
	}
	// The known image goes through to the preview, as before.
	cloud := &imageShelfCloud{cancelCloud: cancelCloud{cancel: func() {}}, images: images}
	r, _ := cancelRegistry(t, cloud)
	text, _ := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "image": "Ubuntu-24.04"})
	if !strings.Contains(text, "confirmation_required") || !strings.Contains(text, catalogUbuntu24) {
		t.Errorf("a known image must reach the preview, resolved to its id: %s", text)
	}
}

// subnet_id of another zone, or not of the project, keeps the VM from
// booting while it and its floating IP are billed. The call refused it
// after consent; the preview showed the configured zone and asked (Grok
// live run on 1.3.0, G130-27). The plan refuses it before the question, as
// the zone without a subnet (D35, D39). A group list that cannot be read
// decides nothing for the preview: the call refuses it when it runs.
func TestProvision_ExplicitSubnetRefusedBeforePreview(t *testing.T) {
	subnets := []*provision.Subnet{
		{ID: "sn-1", Name: "Default_ru.AZ-1", AvailabilityZoneID: "z1", Default: true},
		{ID: "sn-3", Name: "Default_ru.AZ-3", AvailabilityZoneID: "z", Default: true},
		{ID: "sn-n", Name: "Default_ru.AZ-1-legacy"},
	}
	groups := []*provision.SecurityGroup{{ID: "sg-3", Name: "SSH-access_ru.AZ-3", AvailabilityZoneID: "z", Default: true}}
	for name, tc := range map[string]struct {
		subnetID string
		want     string
	}{
		"subnet_id not of the project":         {"sn-x", "subnet sn-x is not in the project"},
		"subnet_id of another zone":            {"sn-1", "subnet Default_ru.AZ-1 is not in zone ru.AZ-3"},
		"subnet_id without zone, foreign name": {"sn-n", "subnet Default_ru.AZ-1-legacy is not in zone ru.AZ-3"},
	} {
		for _, confirmed := range []bool{false, true} {
			t.Run(name+"/confirmed="+boolText(confirmed), func(t *testing.T) {
				cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}, subnets: subnets, groups: groups}
				r := New(func(map[string]any) (*engine.Engine, error) {
					return &engine.Engine{
						Config: config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
						Auth:   staticToken{}, Cloud: cloud,
						Printer:  output.NewWithWriter(&bytes.Buffer{}, output.ModeText, false),
						StateDir: t.TempDir(), SSHKeyPEM: []byte("key"), SSHPubKey: "ssh-ed25519 AAAA test",
					}, nil
				})
				text, isErr := r.handlers["provision"](context.Background(),
					map[string]any{"name": "web", "subnet_id": tc.subnetID, "confirmed": confirmed})
				if !isErr || !strings.Contains(text, tc.want) || !strings.Contains(text, clierrors.CodeConfigInvalid) {
					t.Errorf("want CONFIG_INVALID «%s» before the question: %s", tc.want, text)
				}
				if strings.Contains(text, "confirmation_required") {
					t.Errorf("a preview for a subnet the VM cannot boot in: %s", text)
				}
				if len(cloud.mutations) > 0 {
					t.Errorf("the cloud was changed before the refusal: %v", cloud.mutations)
				}
			})
		}
	}
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// shelfZoneCloud is otherZoneCloud with a catalogue of images: deploy's
// preview reads both.
type shelfZoneCloud struct {
	otherZoneCloud
	images []*provision.Image
}

func (c *shelfZoneCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return c.images, nil
}
func (c *shelfZoneCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	for _, img := range c.images {
		if img.ID == id {
			return &provision.ImageDetail{ID: img.ID, Name: img.Name}, nil
		}
	}
	return nil, clierrors.New(clierrors.CodeNotFound, "HTTP 404: image not found", "")
}
func (*shelfZoneCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, errNoPriceAPI
}

// deploy reads the image in the same preview (engine.completePreview): an
// image the catalogue does not have is NOT_FOUND before the question, and
// a name several images match is USAGE with the candidates — the same
// answer as provision (Qwen review of #345).
func TestDeploy_UnknownImageRefusedBeforePreview(t *testing.T) {
	compose := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(compose, []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		image      string
		reason     string
		candidates bool
	}{
		"no such":   {"NoSuch-1.0", clierrors.CodeNotFound, false},
		"ambiguous": {"Ubuntu", clierrors.CodeUsage, true},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := &shelfZoneCloud{
				otherZoneCloud: otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}},
					subnets: []*provision.Subnet{{ID: "sn-3", Name: "Default_ru.AZ-3", AvailabilityZoneID: "z", Default: true}},
					groups:  []*provision.SecurityGroup{{ID: "sg-3", Name: "SSH-access_ru.AZ-3", AvailabilityZoneID: "z"}}},
				images: []*provision.Image{
					{ID: catalogUbuntu22, Name: "Ubuntu-22.04", DisplayName: "Ubuntu 22.04", Status: "active"},
					{ID: catalogUbuntu24, Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Status: "active"},
				}}
			r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s", ComposeFile: compose})
			m, text, isErr := callNamed(t, r, "deploy", map[string]any{"image": tc.image})
			if !isErr || m["reason"] != tc.reason || m["status"] == "confirmation_required" {
				t.Errorf("deploy image %q: want %s before the question, got %s", tc.image, tc.reason, text)
			}
			if _, ok := m["candidates"]; ok != tc.candidates {
				t.Errorf("candidates present=%v, want %v: %s", ok, tc.candidates, text)
			}
			if len(cloud.mutations) > 0 {
				t.Errorf("the cloud was changed: %v", cloud.mutations)
			}
		})
	}
}

// #470 (а): a flavor the catalogue has not (dogfood 28.10: --flavor
// lowoct10-2-4) is refused before the question, as an image is (D39):
// NOT_FOUND, with the flavors to pick from — not a preview with a price
// and the refusal after the consent.
func TestProvision_UnknownFlavorRefusedBeforePreview(t *testing.T) {
	images := []*provision.Image{{ID: catalogUbuntu24, Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Status: "active"}}
	for _, confirmed := range []bool{false, true} {
		t.Run("confirmed="+boolText(confirmed), func(t *testing.T) {
			cloud := &imageShelfCloud{cancelCloud: cancelCloud{cancel: func() {}}, images: images}
			r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"})
			text, isErr := r.handlers["provision"](context.Background(),
				map[string]any{"name": "web", "image": "Ubuntu-24.04", "flavor": "lowoct10-2-4", "confirmed": confirmed})
			var m map[string]any
			if err := json.Unmarshal([]byte(text), &m); err != nil {
				t.Fatalf("answer is not JSON: %v: %s", err, text)
			}
			if !isErr || m["reason"] != clierrors.CodeNotFound || !strings.Contains(text, "lowoct10-2-4") || !strings.Contains(text, "lowcost10-1-1") {
				t.Errorf("isErr=%v reason=%v, want NOT_FOUND naming the flavor and the ones there are: %s", isErr, m["reason"], text)
			}
			if hint, _ := m["hint"].(string); !strings.Contains(hint, "evo vm flavors") {
				t.Errorf("hint %q: want where to find the flavors", hint)
			}
			if strings.Contains(text, "confirmation_required") || len(cloud.mutations) > 0 || cloud.creates > 0 {
				t.Errorf("a preview or a write for a flavor the catalogue does not have: %s", text)
			}
		})
	}
	// With an image the catalogue has not too: both, the flavor first —
	// the call picks it before the image.
	cloud := &imageShelfCloud{cancelCloud: cancelCloud{cancel: func() {}}, images: images}
	r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"})
	m, text, _ := callNamed(t, r, "provision", map[string]any{"name": "web", "image": "NoSuch-1.0", "flavor": "lowoct10-2-4"})
	refusals, _ := m["refusals"].([]any)
	if len(refusals) != 2 || !strings.Contains(fmt.Sprint(refusals[0]), "lowoct10-2-4") {
		t.Errorf("unknown flavor and image: want both, the flavor's first: %s", text)
	}

	// A flavor by name of the catalogue, and auto, go through.
	for _, f := range []string{"lowcost10-1-1", "auto"} {
		cloud := &imageShelfCloud{cancelCloud: cancelCloud{cancel: func() {}}, images: images}
		r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"})
		if text, _ := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "image": "Ubuntu-24.04", "flavor": f}); !strings.Contains(text, "confirmation_required") {
			t.Errorf("flavor %s must reach the preview: %s", f, text)
		}
	}
}

// #470 (а): deploy refuses a flavor the catalogue has not before the
// question and after it with one answer, provision's (engine.flavorNotFound):
// NOT_FOUND, and nothing changed in the cloud.
func TestDeploy_UnknownFlavorRefusedBeforePreview(t *testing.T) {
	compose := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(compose, []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, confirmed := range []bool{false, true} {
		t.Run("confirmed="+boolText(confirmed), func(t *testing.T) {
			cloud := &shelfZoneCloud{
				otherZoneCloud: otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}},
					subnets: []*provision.Subnet{{ID: "sn-3", Name: "Default_ru.AZ-3", AvailabilityZoneID: "z", Default: true}},
					groups:  []*provision.SecurityGroup{{ID: "sg-3", Name: "SSH-access_ru.AZ-3", AvailabilityZoneID: "z"}}},
				images: []*provision.Image{{ID: catalogUbuntu24, Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Status: "active"}}}
			r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s", ComposeFile: compose})
			m, text, isErr := callNamed(t, r, "deploy", map[string]any{"image": "Ubuntu-24.04", "flavor": "lowoct10-2-4", "confirmed": confirmed})
			if !isErr || m["reason"] != clierrors.CodeNotFound || !strings.Contains(text, "lowoct10-2-4") {
				t.Errorf("deploy with an unknown flavor: want NOT_FOUND, got %s", text)
			}
			if len(cloud.mutations) > 0 {
				t.Errorf("the cloud was changed: %v", cloud.mutations)
			}
		})
	}
}
