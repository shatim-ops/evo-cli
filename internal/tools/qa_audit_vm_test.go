// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Внешний аудит релиза v1.0.0 (#90): provision, destroy, диски.

// fastRegistry — реестр с подделкой облака и миллисекундными ожиданиями.
func fastRegistry(t *testing.T, cloud provision.CloudAPI, cfg config.Config) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    cfg,
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
			Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: time.Second, FIPPollInterval: time.Millisecond,
				FIPRetryInterval: time.Millisecond, DestroyPollInterval: time.Millisecond, DestroyPollTimeout: 20 * time.Millisecond},
		}, nil
	})
	return r, dir
}

var provisionCfg = config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"}

// fipAttachFailsCloud: ВМ стала active, загрузочный FIP платформа не
// привязала, и привязка вручную тоже не удалась.
type fipAttachFailsCloud struct{ pollCheckCloud }

func (*fipAttachFailsCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.9", State: "available"}, nil
}
func (*fipAttachFailsCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{Interfaces: []provision.VMInterface{{ID: "if-1", Primary: true}}}, nil
}
func (*fipAttachFailsCloud) AttachFloatingIP(context.Context, string, string, string, string) (*provision.FloatingIP, error) {
	return nil, clerrors.New(clerrors.CodeAPIError, "API returned status 422: interface_can_not_be_changed", "")
}

// R-02: FIP не привязан — ответ partial с ошибкой, без «SSH: ssh user@IP»
// и без адреса, который ВМ не принадлежит; FIP записан для destroy.
func TestAudit_Provision_FIPAttachFailsIsPartial(t *testing.T) {
	cloud := &fipAttachFailsCloud{}
	r, dir := fastRegistry(t, cloud, provisionCfg)
	cloud.dir = dir
	m, text, isErr := callNamed(t, r, "provision", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["status"] != "partial" || m["reason"] != "API_ERROR" {
		t.Fatalf("want partial API_ERROR, got %s", text)
	}
	if strings.Contains(text, "SSH: ssh") || m["vm_ip"] != "" || strings.Contains(text, "attached after boot") {
		t.Errorf("the answer claims a reachable VM: %s", text)
	}
	if h, _ := m["hint"].(string); !strings.Contains(h, "fip_attach") {
		t.Errorf("hint = %q, want fip_attach", h)
	}
	rec := stateHasVM(t, dir, "vm-made")
	if rec.FloatingIPID != "fip-boot" || rec.VMIP != "" {
		t.Errorf("record = %+v, want floating_ip_id fip-boot and no address", rec)
	}
}

// nameTakenCloud: в проекте уже есть ВМ web (её создал первый вызов
// provision минуту назад), повтор получает 409.
type nameTakenCloud struct{ cancelCloud }

func (*nameTakenCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: "vm-first", Name: "web", CreatedAt: time.Now().Add(-time.Minute)}}, nil
}

// L-01: повтор provision с занятым именем — 409. Чужая ВМ не
// усыновляется, её запись (с её FIP) не перезаписывается, FIP повтора
// освобождается, ответ — USAGE с подсказкой vm_get.
func TestAudit_Provision_NameTaken409DoesNotAdopt(t *testing.T) {
	cloud := &nameTakenCloud{cancelCloud{createErr: clerrors.Wrap(clerrors.CodeAPIError, "API returned status 409", "",
		&provision.StatusError{Status: 409, Body: `{"code":"vm_with_this_data_already_exists"}`})}}
	r, dir := fastRegistry(t, cloud, provisionCfg)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-first", VMID: "vm-first", FloatingIPID: "fip-real", Status: "running"})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	m, text, isErr := callNamed(t, r, "provision", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, "already exists") {
		t.Fatalf("want USAGE «already exists», got %s", text)
	}
	if h, _ := m["hint"].(string); !strings.Contains(h, "vm_get") {
		t.Errorf("hint = %q, want vm_get", h)
	}
	if _, ok := m["vm_id"]; ok {
		t.Errorf("the answer names another VM as created: %s", text)
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-boot" {
		t.Errorf("released FIPs = %v, want the retry's fip-boot", cloud.deletedFIPs)
	}
	if rec := stateHasVM(t, dir, "vm-first"); rec.FloatingIPID != "fip-real" || rec.Status != "running" {
		t.Errorf("the other VM's record changed: %+v", rec)
	}
}

// Отказ API другого рода (422) — тоже без поиска «созданной» ВМ.
func TestAudit_Provision_RejectedCreateReleasesBootFIP(t *testing.T) {
	cloud := &nameTakenCloud{cancelCloud{createErr: clerrors.Wrap(clerrors.CodeAPIError, "API returned status 422", "",
		&provision.StatusError{Status: 422, Body: `{"code":"wrong_image"}`})}}
	r, dir := fastRegistry(t, cloud, provisionCfg)
	m, text, isErr := callNamed(t, r, "provision", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != "API_ERROR" || strings.Contains(text, "vm-first") {
		t.Fatalf("answer = %s", text)
	}
	if len(cloud.deletedFIPs) != 1 {
		t.Errorf("released FIPs = %v", cloud.deletedFIPs)
	}
	if sf, err := state.Read(state.DefaultPath(dir)); err == nil && len(sf.Deployments) != 0 {
		t.Errorf("state records %+v", sf.Deployments)
	}
}

// Финальный аудит, ревью Qwen: отказ create и сбой освобождения
// загрузочного FIP — ответ называет адрес (он оплачивается и нигде не
// записан); раньше предупреждение уходило только в stderr сервера.
func TestAudit_Provision_UnreleasedBootFIPIsNamed(t *testing.T) {
	cloud := &nameTakenCloud{cancelCloud{createErr: clerrors.Wrap(clerrors.CodeAPIError, "API returned status 422", "",
		&provision.StatusError{Status: 422, Body: `{"code":"wrong_image"}`}), fipDelErr: errors.New("API 503: unavailable")}}
	r, _ := fastRegistry(t, cloud, provisionCfg)
	m, text, isErr := callNamed(t, r, "provision", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != "API_ERROR" || m["floating_ip_id"] != "fip-boot" || !strings.Contains(text, "release_floating_ip") {
		t.Fatalf("answer = %s", text)
	}
}

// previewCloud: targetCloud с зоной и адресом у ВМ; считает удаления.
type previewCloud struct {
	targetCloud
	vms     []*provision.VM
	deletes int
}

func (c *previewCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return c.vms, nil
}
func (*previewCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}
func (c *previewCloud) DeleteVMInProject(ctx context.Context, id, p string) error {
	c.deletes++
	return c.targetCloud.DeleteVMInProject(ctx, id, p)
}

// R-04: превью удаления называет цель — id, имя, зону, состояние, адрес —
// и какой FIP будет освобождён; ничего не удаляется.
func TestAudit_DestroyPreviewNamesTarget(t *testing.T) {
	cloud := &previewCloud{vms: []*provision.VM{{ID: "0000a1a1-0000-4000-8000-000000000001", Name: "web", Status: "running",
		AvailabilityZone: &provision.ZoneRef{ID: "z-2", Name: "ru.AZ-2"}}}}
	r, _ := targetRegistry(t, cloud)
	for _, args := range []map[string]any{{"vm_id": "0000a1a1-0000-4000-8000-000000000001"}, {"name": "web"}} {
		m, text, isErr := callNamed(t, r, "destroy", args)
		if !isErr || m["status"] != "confirmation_required" {
			t.Fatalf("%v: %s", args, text)
		}
		tgt, _ := m["target"].(map[string]any)
		if tgt["id"] != "0000a1a1-0000-4000-8000-000000000001" || tgt["name"] != "web" || tgt["zone"] != "ru.AZ-2" || tgt["status"] != "running" || tgt["public_ip"] != "203.0.113.5" {
			t.Errorf("%v: target = %v", args, tgt)
		}
		if f, _ := m["floating_ip"].(string); !strings.Contains(f, "will be released") || !strings.Contains(f, "203.0.113.5 (fip-1)") {
			t.Errorf("%v: floating_ip = %q", args, f)
		}
		if f, _ := m["floating_ip"].(string); strings.Contains(f, "released:") && !strings.Contains(f, "will be released:") {
			t.Errorf("%v: preview uses past tense: %q", args, f)
		}
		if msg, _ := m["message"].(string); !strings.Contains(msg, "web (0000a1a1-0000-4000-8000-000000000001) in zone ru.AZ-2") {
			t.Errorf("%v: message = %q", args, msg)
		}
		if m["project_id"] != "p" || !strings.Contains(text, "release_ip=true") {
			t.Errorf("%v: %s", args, text)
		}
	}
	if cloud.deletes != 0 || cloud.fipTries != 0 {
		t.Errorf("the preview deleted something: %d VM, %d FIP", cloud.deletes, cloud.fipTries)
	}
}

// R-04, A-004, D26: ВМ, которой нет ни в облаке, ни в записях, — уже
// удалена (код 0, already_deleted) уже на превью, без вопроса; id не того
// вида — USAGE до облака; без проекта — CONFIG_INVALID.
func TestAudit_DestroyPreviewNotFoundAndNoProject(t *testing.T) {
	cloud := &previewCloud{vms: []*provision.VM{{ID: "0000a1a1-0000-4000-8000-000000000001", Name: "web"}}}
	r, _ := targetRegistry(t, cloud)
	for _, args := range []map[string]any{{"name": "nope"}, {"vm_id": "0000a1a1-0000-4000-8000-000000000404"}, {"name": "nope", "confirmed": true}} {
		m, text, isErr := callNamed(t, r, "destroy", args)
		if isErr || m["status"] != "already_deleted" || m["existed"] != false {
			t.Errorf("%v: %s", args, text)
		}
	}
	if m, text, isErr := callNamed(t, r, "destroy", map[string]any{"vm_id": "vm-404"}); !isErr || m["reason"] != "USAGE" {
		t.Errorf("vm_id of the wrong form: %s", text)
	}
	r, _ = fastRegistry(t, cloud, config.Config{KeyID: "k", Secret: "s"})
	m, text, isErr := callNamed(t, r, "destroy", map[string]any{"name": "web"})
	if !isErr || m["reason"] != "CONFIG_INVALID" {
		t.Errorf("no project: %s", text)
	}
	if cloud.deletes != 0 {
		t.Errorf("deleted %d VMs", cloud.deletes)
	}
}

// R-03: две ВМ с одним именем — USAGE со списком кандидатов (id, зона),
// ничего не удаляется; vm_id выбирает нужную.
func TestAudit_DestroyAmbiguousName(t *testing.T) {
	cloud := &previewCloud{vms: []*provision.VM{
		{ID: "0000a1a1-0000-4000-8000-00000000000a", Name: "web", AvailabilityZone: &provision.ZoneRef{ID: "z-1", Name: "ru.AZ-1"}},
		{ID: "0000a1a1-0000-4000-8000-00000000000b", Name: "web", AvailabilityZone: &provision.ZoneRef{ID: "z-2", Name: "ru.AZ-2"}},
	}}
	r, _ := targetRegistry(t, cloud)
	for _, confirmed := range []bool{false, true} {
		m, text, isErr := callNamed(t, r, "destroy", map[string]any{"name": "web", "confirmed": confirmed})
		if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, "0000a1a1-0000-4000-8000-00000000000a (zone ru.AZ-1)") || !strings.Contains(text, "0000a1a1-0000-4000-8000-00000000000b (zone ru.AZ-2)") {
			t.Fatalf("confirmed=%v: %s", confirmed, text)
		}
		if c, _ := m["candidates"].([]any); len(c) != 2 {
			t.Errorf("candidates = %v", m["candidates"])
		}
		if h, _ := m["hint"].(string); !strings.Contains(h, "vm_id") {
			t.Errorf("hint = %q", h)
		}
	}
	if cloud.deletes != 0 {
		t.Fatalf("deleted %d VMs on an ambiguous name", cloud.deletes)
	}
	m, text, isErr := callNamed(t, r, "destroy", map[string]any{"vm_id": "0000a1a1-0000-4000-8000-00000000000b", "confirmed": true})
	if isErr || m["deleted_vm"] != "0000a1a1-0000-4000-8000-00000000000b" || cloud.deletes != 1 {
		t.Errorf("vm_id: %s", text)
	}
}

// stuckTargetCloud: удаление принято, но ВМ так и остаётся в списке.
type stuckTargetCloud struct {
	targetCloud
	ruleDeletes int
}

func (*stuckTargetCloud) DeleteVMInProject(context.Context, string, string) error { return nil }
func (*stuckTargetCloud) DeleteVM(context.Context, string) error                  { return nil }
func (*stuckTargetCloud) GetVM(_ context.Context, id string) (*provision.VM, error) {
	return &provision.VM{ID: id, Status: "deleting"}, nil
}
func (c *stuckTargetCloud) DeleteSecurityGroupRule(context.Context, string, string) error {
	c.ruleDeletes++
	return nil
}

// withRule добавляет записи vm-1 правило порта 80, открытое evo.
func withRule(t *testing.T, dir string) {
	t.Helper()
	path := state.DefaultPath(dir)
	sf, err := state.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := sf.FindDeployment("0000a1a1-0000-4000-8000-000000000001")
	rec.SecurityGroupID = "sg-1"
	rec.OpenedRules = []state.SGRule{{ID: "rule-80", SecurityGroupID: "sg-1", Port: "tcp/80"}}
	if err := state.Write(path, sf); err != nil {
		t.Fatal(err)
	}
}

// A-005, L-11: удаление не подтвердилось — ответ partial с ошибкой
// (API_ERROR, код 5), запись остаётся (destroying, с FIP), FIP не
// освобождается и правила портов не закрываются; повтор после того, как
// ВМ пропала, доделывает работу.
func TestAudit_DestroyTargetedUnconfirmedIsPartial(t *testing.T) {
	cloud := &stuckTargetCloud{}
	r, dir := targetRegistry(t, cloud)
	withRule(t, dir)
	m, text, isErr := callNamed(t, r, "destroy", map[string]any{"vm_id": "0000a1a1-0000-4000-8000-000000000001", "confirmed": true})
	if !isErr || m["status"] != "partial" || m["reason"] != "API_ERROR" {
		t.Fatalf("want partial API_ERROR, got %s", text)
	}
	if h, _ := m["hint"].(string); !strings.Contains(h, "destroy") || !strings.Contains(h, "again") {
		t.Errorf("hint = %q, want a re-run", h)
	}
	if cloud.fipTries != 0 || cloud.ruleDeletes != 0 {
		t.Errorf("released while the VM still exists: %d FIP tries, %d rule deletes", cloud.fipTries, cloud.ruleDeletes)
	}
	rec := stateHasVM(t, dir, "0000a1a1-0000-4000-8000-000000000001")
	if rec.Status != "destroying" || rec.FloatingIPID != "fip-1" || len(rec.OpenedRules) != 1 {
		t.Errorf("record = %+v, want destroying with its FIP and rule", rec)
	}

	// Повтор: ВМ уже нет ни в списке, ни по GET — запись снимается, FIP
	// освобождается, правило закрывается.
	gone := &goneCloud{}
	r2 := New(func(map[string]any) (*engine.Engine, error) {
		eng, err := r.newEngine(nil)
		if err == nil {
			eng.Cloud = &goneRuleCloud{goneCloud: gone}
		}
		return eng, err
	})
	m, text, isErr = callNamed(t, r2, "destroy", map[string]any{"vm_id": "0000a1a1-0000-4000-8000-000000000001", "confirmed": true})
	if isErr || m["status"] != "already_deleted" || gone.releasedFIP != "fip-1" {
		t.Fatalf("re-run: %s (released %q)", text, gone.releasedFIP)
	}
	sf, _ := state.Read(state.DefaultPath(dir))
	if a := sf.ActiveDeployment(); a != nil {
		t.Errorf("record left after the re-run: %+v", a)
	}
}

// Ревью Cursor по #89: release_ip=false и удаление не подтвердилось —
// подсказка повтора сохраняет release_ip=false, иначе повтор с умолчанием
// release_ip=true освободил бы адрес, который просили оставить.
func TestAudit_DestroyUnconfirmedKeepsReleaseIPFalseInHint(t *testing.T) {
	cloud := &stuckTargetCloud{}
	r, dir := targetRegistry(t, cloud)
	withRule(t, dir)
	m, text, isErr := callNamed(t, r, "destroy", map[string]any{"vm_id": "0000a1a1-0000-4000-8000-000000000001", "confirmed": true, "release_ip": false})
	if !isErr || m["status"] != "partial" {
		t.Fatalf("want partial, got %s", text)
	}
	if h, _ := m["hint"].(string); !strings.Contains(h, "release_ip=false") {
		t.Errorf("hint = %q, want the re-run with release_ip=false", h)
	}
	if cloud.fipTries != 0 {
		t.Errorf("FIP touched: %d tries", cloud.fipTries)
	}
}

type goneRuleCloud struct{ *goneCloud }

func (*goneRuleCloud) DeleteSecurityGroupRule(context.Context, string, string) error { return nil }

// A-005: то же у destroy текущего развёртывания (MCP) — partial, не ok.
func TestAudit_DestroyCurrentUnconfirmedIsPartial(t *testing.T) {
	cloud := &stuckTargetCloud{}
	r, dir := targetRegistry(t, cloud)
	m, text, isErr := callNamed(t, r, "destroy", map[string]any{"confirmed": true})
	if !isErr || m["status"] != "partial" || m["reason"] != "API_ERROR" {
		t.Fatalf("want partial API_ERROR, got %s", text)
	}
	if cloud.fipTries != 0 {
		t.Errorf("FIP released while the VM still exists")
	}
	if rec := stateHasVM(t, dir, "0000a1a1-0000-4000-8000-000000000001"); rec.Status != "destroying" || rec.FloatingIPID != "fip-1" {
		t.Errorf("record = %+v", rec)
	}
}

// A-032: ошибка logs, до которой ничего не напечатано, — без пустого
// "log": "".
func TestAudit_LogsErrorWithoutEmptyLog(t *testing.T) {
	r, _ := fastRegistry(t, &targetCloud{}, config.Config{ProjectID: "p"})
	m, text, isErr := callNamed(t, r, "logs", map[string]any{})
	if !isErr || m["reason"] != "VM_NOT_FOUND" {
		t.Fatalf("logs without a deployment: %s", text)
	}
	if _, ok := m["log"]; ok {
		t.Errorf("empty log in the answer: %s", text)
	}
}

// diskCloud: новый диск сначала creating; привязка до available — 422
// «Disk has no openstack_id», как живьём.
type diskCloud struct {
	provision.CloudAPI
	lists     int
	readyAt   int
	stuck     string // the state the disk stays in, when set
	attachErr error
	attached  bool
}

func (*diskCloud) SetToken(string) {}
func (*diskCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: "0000a1a1-0000-4000-8000-000000000001", Name: "web", Region: "z"}}, nil
}
func (*diskCloud) CreateDisk(context.Context, provision.CreateDiskReq) (map[string]any, error) {
	return map[string]any{"id": "d-1", "name": "data", "state": "creating"}, nil
}
func (c *diskCloud) state() string {
	if c.stuck != "" {
		return c.stuck
	}
	if c.readyAt > 0 && c.lists >= c.readyAt {
		return "available"
	}
	return "creating"
}
func (c *diskCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	c.lists++
	return []map[string]any{{"id": "d-0", "state": "available"}, {"id": "d-1", "state": c.state()}}, nil
}
func (c *diskCloud) AttachDisk(context.Context, string, string, string) (map[string]any, error) {
	if c.attachErr != nil {
		return nil, c.attachErr
	}
	if c.state() != "available" {
		return nil, clerrors.Wrap(clerrors.CodeAPIError, "API returned status 422: Disk has no openstack_id", "",
			&provision.StatusError{Status: 422, Body: "Disk has no openstack_id"})
	}
	c.attached = true
	return map[string]any{"status": "attaching"}, nil
}

// L-24: disk_create attach_to ждёт, пока диск станет available, и только
// потом привязывает его.
func TestAudit_DiskCreateAttachWaitsForAvailable(t *testing.T) {
	cloud := &diskCloud{readyAt: 3}
	r, _ := fastRegistry(t, cloud, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"})
	m, text, isErr := callNamed(t, r, "disk_create", map[string]any{"name": "data", "size_gb": 10, "disk_type": "SSD", "attach_to": "web", "confirmed": true})
	if isErr || m["status"] != "created" || !cloud.attached || cloud.lists < 3 {
		t.Fatalf("attached=%v after %d polls: %s", cloud.attached, cloud.lists, text)
	}
}

// Живой прогон 18.10: disk_attach сразу после disk_create — 422 «Disk has
// no openstack_id» (CMP-15), хотя протокол догфуда обещает, что evo ждёт.
// disk_attach ждёт available, как disk_create attach_to (L-24), и
// привязывает; диск, который так и не стал available, — ошибка с причиной.
func TestDiskAttach_WaitsForNewDisk(t *testing.T) {
	cloud := &diskCloud{readyAt: 3}
	r, _ := fastRegistry(t, cloud, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"})
	m, text, isErr := callNamed(t, r, "disk_attach", map[string]any{"disk_id": "d-1", "vm": "web", "confirmed": true})
	if isErr || m["status"] != "attached" || !cloud.attached {
		t.Fatalf("attached=%v after %d polls: %s", cloud.attached, cloud.lists, text)
	}

	stuck := &diskCloud{stuck: "creating"}
	r, _ = fastRegistry(t, stuck, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"})
	m, text, isErr = callNamed(t, r, "disk_attach", map[string]any{"disk_id": "d-1", "vm": "web", "confirmed": true})
	if !isErr || stuck.attached || m["reason"] != "API_ERROR" || !strings.Contains(text, "did not become available") {
		t.Fatalf("disk never available: %s", text)
	}

	// Другая ошибка привязки — без ожидания: список дисков не читается.
	busy := &diskCloud{readyAt: 1, attachErr: clerrors.New(clerrors.CodeAPIError, "API returned status 422: busy", "")}
	r, _ = fastRegistry(t, busy, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"})
	if _, text, isErr := callNamed(t, r, "disk_attach", map[string]any{"disk_id": "d-1", "vm": "web", "confirmed": true}); !isErr || busy.lists != 0 {
		t.Fatalf("other attach error waited (%d polls): %s", busy.lists, text)
	}
}

// L-24: привязка не удалась (или диск не стал available) — partial с
// ошибкой, disk_id в ответе, подсказка disk_attach.
func TestAudit_DiskCreateAttachFailsIsPartial(t *testing.T) {
	for name, cloud := range map[string]*diskCloud{
		"attach fails":     {readyAt: 1, attachErr: clerrors.New(clerrors.CodeAPIError, "API returned status 422: busy", "")},
		"never available":  {stuck: "creating"},
		"disk in an error": {stuck: "error_creating"},
	} {
		r, _ := fastRegistry(t, cloud, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"})
		m, text, isErr := callNamed(t, r, "disk_create", map[string]any{"name": "data", "size_gb": 10, "disk_type": "SSD", "attach_to": "web", "confirmed": true})
		if !isErr || m["status"] != "partial" || m["disk_id"] != "d-1" || m["reason"] != "API_ERROR" {
			t.Errorf("%s: %s", name, text)
		}
		if h, _ := m["hint"].(string); !strings.Contains(h, "disk_attach disk_id=d-1") {
			t.Errorf("%s: hint = %q", name, h)
		}
	}
}

// R-04: превью k8s_delete, pg_delete, kafka_delete, redis_delete,
// ar_delete называет ресурс по одному GET — имя и состояние, не только id —
// и ничего не меняет.
func TestAudit_ServiceDeletePreviewsNameTheResource(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000001"
	rec := &recorder{body: `{"id":"` + id + `","name":"prod-db","status":"RUNNING"}`}
	old := http.DefaultTransport
	http.DefaultTransport = rec
	t.Cleanup(func() { http.DefaultTransport = old })
	r, _ := fastRegistry(t, &targetCloud{}, config.Config{ProjectID: id, Region: id, KeyID: "k", Secret: "s"})
	for tool, arg := range map[string]string{"k8s_delete": "cluster_id", "pg_delete": "id", "kafka_delete": "cluster_id",
		"redis_delete": "cluster_id", "ar_delete": "id"} {
		m, text, isErr := callNamed(t, r, tool, map[string]any{arg: id})
		if !isErr || m["status"] != "confirmation_required" {
			t.Errorf("%s: %s", tool, text)
			continue
		}
		if msg, _ := m["message"].(string); !strings.Contains(msg, `"prod-db" (`+id+")") {
			t.Errorf("%s: message %q does not name the resource", tool, msg)
		}
		if tgt, _ := m["target"].(map[string]any); tgt["name"] != "prod-db" || tgt["status"] != "RUNNING" {
			t.Errorf("%s: target = %v", tool, m["target"])
		}
		for _, q := range rec.take() {
			if !strings.HasPrefix(q, "GET ") && !strings.Contains(q, "iam") {
				t.Errorf("%s preview sent %s", tool, q)
			}
		}
	}
}
