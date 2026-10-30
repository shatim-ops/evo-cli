// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Агентный прогон M2 на rc.3 (30.10): клиент MCP обрывает tools/call через
// 60 с, а ВМ уже создаётся. Агент повторяет вызов — второй ВМ быть не
// должно (CLAUDE.md §1: повторный create не плодит дублей).

// listingCancelCloud — cancelCloud, у которого созданная ВМ видна в списке
// проекта, как в облаке после POST создания, а её имя занято (409);
// вызовы после отмены отвечают ошибкой контекста, как клиент HTTP.
type listingCancelCloud struct {
	*cancelCloud
	made []*provision.VM
}

func (c *listingCancelCloud) CreateVM(ctx context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	// Имя ВМ в проекте занято — 409, как отвечает Cloud.ru (CMP-13).
	for _, vm := range c.made {
		if vm.Name == req.Name {
			return nil, &provision.StatusError{Status: 409, Body: "vm with this name already exists"}
		}
	}
	vm, err := c.cancelCloud.CreateVM(ctx, req)
	if err == nil {
		c.made = append(c.made, &provision.VM{ID: vm.ID, Name: req.Name, Status: "creating"})
	}
	return vm, err
}

func (c *listingCancelCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return c.made, nil
}

// GetImage: образ Linux с пользователем для входа — deploy без него
// отказывает до создания (как ubuntuCloud).
func (c *listingCancelCloud) GetImage(ctx context.Context, id string) (*provision.ImageDetail, error) {
	return (&ubuntuCloud{}).GetImage(ctx, id)
}

// ListDiskTypes: deploy выбирает тип загрузочного диска до создания.
func (*listingCancelCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return []*provision.DiskType{{ID: "dt-ssd", Name: "SSD"}}, nil
}

// GetFloatingIP: загрузочный адрес привязан к ВМ.
func (*listingCancelCloud) GetFloatingIP(ctx context.Context, id string) (*provision.FloatingIP, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &provision.FloatingIP{ID: id, IPAddress: "203.0.113.9", State: "attached"}, nil
}

// AddSecurityGroupRule: запрос с отменённым контекстом не уходит, как у
// настоящего клиента HTTP.
func (*listingCancelCloud) AddSecurityGroupRule(ctx context.Context, _, _ string, rule provision.SGRule) (*provision.SGRule, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &rule, nil
}

// GetVM: ВМ из списка становится активной — ожидание после повтора не
// виснет.
func (c *listingCancelCloud) GetVM(ctx context.Context, id string) (*provision.VM, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, vm := range c.made {
		if vm.ID == id {
			return &provision.VM{ID: vm.ID, Name: vm.Name, Status: "running", PublicIP: "203.0.113.9"}, nil
		}
	}
	return nil, errors.New("404 vm not found")
}

// Повтор provision с тем же именем после отмены не создаёт вторую ВМ.
func TestCancel_ProvisionRepeatAfterCancelMakesNoSecondVM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &listingCancelCloud{cancelCloud: &cancelCloud{cancel: cancel}}
	r, _ := cancelRegistry(t, cloud)
	if _, isErr := r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true}); !isErr {
		t.Fatal("первый вызов не прерван")
	}
	cloud.cancel = func() {}
	text, _ := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if cloud.creates != 1 {
		t.Fatalf("повтор после отмены создал ВМ ещё раз: CreateVM вызван %d раз; ответ: %s", cloud.creates, text)
	}
	// Повтор отказывает 409 → USAGE и называет имя: подсказка ведёт к ВМ
	// прерванного вызова (vm_get), а не к новому create.
	if !strings.Contains(text, "USAGE") || !strings.Contains(text, "web") {
		t.Errorf("повтор не говорит, что ВМ с этим именем уже есть: %s", text)
	}
}

// noSSH: SSH к ВМ не поднимается — deploy после ВМ ждёт SSH до отмены.
type noSSH struct{ deploy.Deployer }

func (noSSH) Connect(ctx context.Context, _ string, _ int, _ string, _ []byte, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("ssh: connection timed out")
}

// deployCancelRegistry — cancelRegistry с Deployer: повтор deploy доходит
// до SSH к уже созданной ВМ.
func deployCancelRegistry(t *testing.T, cloud provision.CloudAPI) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	compose := writeTestCompose(t)
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s", ComposeFile: compose},
			Auth:      staticToken{},
			Cloud:     cloud,
			Deployer:  noSSH{},
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
		}, nil
	})
	return r, dir
}

// MCP-инструмент deploy (не deploy_apply): отмена после POST создания —
// ответ называет ВМ, state её содержит; повтор берёт ВМ из state и не
// создаёт вторую.
func TestCancel_DeployAfterCreateRecordsVMAndRepeatMakesNoSecond(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &listingCancelCloud{cancelCloud: &cancelCloud{cancel: cancel}}
	r, dir := deployCancelRegistry(t, cloud)
	text, isErr := r.handlers["deploy"](ctx, map[string]any{"confirmed": true})
	if !isErr || !strings.Contains(text, "vm-made") {
		t.Fatalf("ответ deploy не называет ВМ: %s (creates %d)", text, cloud.creates)
	}
	stateHasVM(t, dir, "vm-made")
	cloud.cancel = func() {}
	// Повтор ждёт SSH к той же ВМ; ожидание обрывает срок вызова.
	rctx, rcancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer rcancel()
	text, _ = r.handlers["deploy"](rctx, map[string]any{"confirmed": true})
	if cloud.creates != 1 {
		t.Fatalf("повтор deploy после отмены создал ВМ ещё раз: CreateVM вызван %d раз; ответ: %s", cloud.creates, text)
	}
}
