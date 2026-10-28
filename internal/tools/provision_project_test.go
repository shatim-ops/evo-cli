// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// provision из образа, которого проект не видит (GET /images?project_id=
// его не отдаёт): отказ NOT_FOUND до любых изменений в облаке — иначе
// создание ВМ ответит 422 wrong_image. ID выдуманы.

// hiddenImageCloud — общий каталог с образом img-1 «ubuntu», которого нет
// в каталоге проекта; любая мутация считается.
type hiddenImageCloud struct {
	cancelCloud
	mutations []string
	projected []provision.ImageFilter
}

func (c *hiddenImageCloud) ListImages(_ context.Context, f ...provision.ImageFilter) ([]*provision.Image, error) {
	if len(f) > 0 && f[0].ProjectID != "" {
		c.projected = append(c.projected, f[0])
		return nil, nil
	}
	return []*provision.Image{{ID: "img-1", Name: "ubuntu", Type: "marketplace",
		AvailZones: []provision.ImageAvailZone{{AvailabilityZoneID: "z", State: "created"}}}}, nil
}
func (*hiddenImageCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, nil
}
func (c *hiddenImageCloud) CreateFloatingIP(ctx context.Context, r provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.mutations = append(c.mutations, "CreateFloatingIP")
	return c.cancelCloud.CreateFloatingIP(ctx, r)
}
func (c *hiddenImageCloud) CreateVM(ctx context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.mutations = append(c.mutations, "CreateVM")
	return c.cancelCloud.CreateVM(ctx, req)
}

// imageRegistry is cancelRegistry whose engines take the call's image or
// image_id, as the configuration does.
func imageRegistry(t *testing.T, cloud provision.CloudAPI) *Registry {
	t.Helper()
	dir := t.TempDir()
	compose := writeTestCompose(t)
	return New(func(o map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		cfg := config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s", ComposeFile: compose}
		if v, ok := o["Image"].(string); ok {
			cfg.ImageID, cfg.Image = "", v
		}
		if v, ok := o["ImageID"].(string); ok {
			cfg.ImageID = v
		}
		return &engine.Engine{
			Config: cfg, Auth: staticToken{}, Cloud: cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir, SSHKeyPEM: []byte("key"), SSHPubKey: "ssh-ed25519 AAAA test",
		}, nil
	})
}

func TestProvision_ImageNotInProjectRefusedBeforeAnyMutation(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"image_id из конфигурации": {},
		"image по имени":           {"image": "ubuntu"},
		"image_id аргументом":      {"image_id": "img-1"},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := &hiddenImageCloud{cancelCloud: cancelCloud{cancel: func() {}}}
			r := imageRegistry(t, cloud)
			args["name"], args["confirmed"] = "web", true
			text, isErr := r.handlers["provision"](context.Background(), args)
			if !isErr {
				t.Fatalf("ждали отказ: %s", text)
			}
			var res map[string]any
			_ = json.Unmarshal([]byte(text), &res)
			if res["reason"] != "NOT_FOUND" || !strings.Contains(res["error"].(string), `image "ubuntu" is in the Cloud.ru catalogue but not available to the project`) ||
				!strings.Contains(res["hint"].(string), "evo image list <query>") || !strings.Contains(res["hint"].(string), "unverified") {
				t.Errorf("ответ: %s", text)
			}
			if len(cloud.mutations) > 0 {
				t.Errorf("до отказа облако изменено: %v", cloud.mutations)
			}
			if len(cloud.projected) == 0 || cloud.projected[0].ProjectID != "p" || cloud.projected[0].Name != "ubuntu" {
				t.Errorf("каталог проекта не спрошен: %+v", cloud.projected)
			}
		})
	}
}

// Превью provision и deploy читают образ (spec REQ-7): недоступный проекту
// образ — отказ NOT_FOUND уже на превью, как у evo vm create, до вопроса
// и без изменений в облаке; раньше отказ приходил после согласия.
func TestPreview_ImageNotInProjectRefused(t *testing.T) {
	for tool, args := range map[string]map[string]any{
		"provision": {"name": "web", "image": "ubuntu"},
		"deploy":    {"image": "ubuntu"},
	} {
		t.Run(tool, func(t *testing.T) {
			cloud := &hiddenImageCloud{cancelCloud: cancelCloud{cancel: func() {}}}
			r := imageRegistry(t, cloud)
			res, text, isErr := callNamed(t, r, tool, args)
			msg, _ := res["error"].(string)
			if !isErr || res["status"] == "confirmation_required" || res["reason"] != "NOT_FOUND" ||
				!strings.Contains(msg, "not available to the project") {
				t.Fatalf("ответ: %s", text)
			}
			if len(cloud.mutations) > 0 {
				t.Errorf("превью изменило облако: %v", cloud.mutations)
			}
		})
	}
}

// noGroupCloud — ВМ создаётся и сразу активна, групп безопасности в
// проекте нет; правило порта не должно создаваться (встроенный nil
// CloudAPI уронит тест на AddSecurityGroupRule).
type noGroupCloud struct {
	cancelCloud
}

func (c *noGroupCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	c.createReq = req
	return &provision.VM{ID: "vm-made", Status: "creating"}, nil
}
func (*noGroupCloud) GetVM(_ context.Context, id string) (*provision.VM, error) {
	return &provision.VM{ID: id, Status: "running"}, nil
}

// provision, когда в проекте нет групп безопасности: отказ до создания
// ВМ, как у deploy (ревью Fable на v1.0) — ВМ без группы платная и
// недоступна по SSH.
func (*noGroupCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return nil, nil
}

func TestProvision_NoSecurityGroupRefused(t *testing.T) {
	cloud := &noGroupCloud{cancelCloud: cancelCloud{cancel: func() {}}}
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
			Auth:   staticToken{}, Cloud: cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir, SSHKeyPEM: []byte("key"), SSHPubKey: "ssh-ed25519 AAAA test",
			Timings: engine.Timings{VMPollInterval: time.Millisecond},
		}, nil
	})
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{
		"name": "web", "confirmed": true, "no_public_ip": true, "ports": []any{"80", "53/udp"}})
	if !isErr || !strings.Contains(text, "CONFIG_INVALID") || cloud.creates != 0 {
		t.Fatalf("ответ %s, creates %d", text, cloud.creates)
	}
}
