// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/stack"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Вызов MCP отменяют (клиент ушёл, notifications/cancelled, остановка
// сервера) уже после того, как запрос создания ушёл в облако. ВМ есть и
// оплачивается: ответ должен её назвать, state — содержать, а повтор — не
// создавать вторую.

// cancelCloud создаёт ВМ и тут же отменяет контекст вызова — как клиент,
// ушедший сразу после POST создания.
type cancelCloud struct {
	provision.CloudAPI
	cancel      context.CancelFunc
	creates     int
	createReq   provision.CreateVMRequest
	createErr   error
	fipErr      error
	fipDelErr   error
	deletedFIPs []string
}

// ListDiskTypes: the plan prices the preview before the question (D35);
// the price is not this fake's concern.
func (*cancelCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return nil, errors.New("no disk types in this fake")
}

func (c *cancelCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	if c.fipErr != nil {
		return nil, c.fipErr
	}
	return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.9", State: "available"}, nil
}
func (*cancelCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return nil, nil
}
func (c *cancelCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	if c.fipDelErr != nil {
		return c.fipDelErr
	}
	c.deletedFIPs = append(c.deletedFIPs, id)
	return nil
}

func (*cancelCloud) SetToken(string) {}
func (*cancelCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{{ID: "fl-1", Name: "lowcost10-1-1", VCPUs: 1, RAMGB: 1}}, nil
}
func (*cancelCloud) GetImage(context.Context, string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: "img-1", Name: "ubuntu"}, nil
}
func (*cancelCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return []*provision.Image{{ID: "img-1", Name: "ubuntu"}}, nil
}

// ListAvailabilityZones lists the zone "z" the tests' configurations name:
// a zone the list lacks is refused before anything is created (CUR122-4).
func (*cancelCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: "z"}}, nil
}
func (*cancelCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{{ID: "sn-1", Name: "default"}}, nil
}

// Группа по умолчанию есть в каждом проекте Cloud.ru: без неё provision
// отказывает, как deploy.
func (*cancelCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "sg-1", Name: "default", Default: true}}, nil
}
func (*cancelCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, nil
}
func (c *cancelCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	c.createReq = req
	if c.createErr != nil {
		return nil, c.createErr
	}
	c.cancel()
	return &provision.VM{ID: "vm-made"}, nil
}

func cancelRegistry(t *testing.T, cloud provision.CloudAPI) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	compose := writeTestCompose(t)
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s", ComposeFile: compose},
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
		}, nil
	})
	return r, dir
}

// writeTestCompose writes a compose file of one web service: deploy reads
// it before its preview, as before its question (D48).
func writeTestCompose(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(path, []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func stateHasVM(t *testing.T, dir, vmID string) *state.DeploymentRecord {
	t.Helper()
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range sf.Deployments {
		if sf.Deployments[i].VMID == vmID {
			return &sf.Deployments[i]
		}
	}
	t.Fatalf("ВМ %s нет в state: %+v", vmID, sf.Deployments)
	return nil
}

func TestCancel_ProvisionAfterCreateNamesAndRecordsVM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &cancelCloud{cancel: cancel}
	r, dir := cancelRegistry(t, cloud)
	text, isErr := r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true})
	if !isErr || !strings.Contains(text, "vm-made") || !strings.Contains(text, `"vm_id": "vm-made"`) {
		t.Fatalf("ответ не называет ВМ: %s", text)
	}
	rec := stateHasVM(t, dir, "vm-made")
	if rec.Status != "creating" {
		t.Errorf("status в state = %q", rec.Status)
	}
	// Публичный IP — в запросе создания (ВМ грузится с интернетом), а в
	// state — его ID, чтобы destroy его освободил.
	if got := cloud.createReq.Interfaces[0].AttachExternalIPID; got != "fip-boot" {
		t.Errorf("запрос создания привязывает %q, ожидался fip-boot", got)
	}
	if rec.FloatingIPID != "fip-boot" {
		t.Errorf("FloatingIPID в state = %q", rec.FloatingIPID)
	}
}

// ВМ не создана: зарезервированный под неё IP не остаётся оплачиваться.
func TestProvision_CreateFails_ReleasesBootFIP(t *testing.T) {
	cloud := &cancelCloud{cancel: func() {}, createErr: errors.New("422 quota")}
	r, _ := cancelRegistry(t, cloud)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !isErr || !strings.Contains(text, "422") {
		t.Fatalf("ответ: %s", text)
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-boot" {
		t.Errorf("освобождены %v, ожидался [fip-boot]", cloud.deletedFIPs)
	}
}

func TestCancel_DeployApplyAfterCreateRecordsVMAndKeepsPlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &cancelCloud{cancel: cancel}
	r, dir := cancelRegistry(t, cloud)
	plan := &deployplan.Plan{PlanID: "plan-0123456789ab", ExpiresAt: time.Now().Add(time.Hour), VM: deployplan.VMSpec{Name: "web"}}
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["deploy_apply"](ctx, map[string]any{"plan_id": plan.PlanID, "confirmed": true})
	if !isErr || !strings.Contains(text, "vm-made") {
		t.Fatalf("ответ не называет ВМ: %s", text)
	}
	if rec := stateHasVM(t, dir, "vm-made"); rec.Status != "creating" {
		t.Errorf("status в state = %q", rec.Status)
	}
	// План остаётся: повтор с тем же plan_id подхватывает ВМ по имени.
	if _, err := deployplan.Load(dir, plan.PlanID); err != nil {
		t.Errorf("план удалён после прерывания: %v", err)
	}
}

// Шаг vm стека: provision прервали после создания — шаг помнит ВМ, попытка
// не считается, следующий crank берёт ВМ, а не создаёт вторую.
func TestCancel_StackStepInterruptedAdoptsOnNextCrank(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"i","vm":{"name":"v"}}`)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	r.handlers["provision"] = func(context.Context, map[string]any) (string, bool) {
		calls++
		cancel()
		return toJSON(map[string]any{"status": "error", "vm_id": "vm-9", "error": "interrupted"}), true
	}
	r.handleStackApply(ctx, map[string]any{"job_id": j.ID, "confirmed": true})
	job, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	st := job.Steps[0]
	if !st.Interrupted || st.ResourceID != "vm-9" || st.Attempts != 0 || st.Status != "pending" {
		t.Fatalf("после отмены: %+v", st)
	}
	r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	job, _ = stack.LoadJob(j.ID)
	if calls != 1 || job.Outputs["vm"] != "vm-9" || job.Steps[0].Status != "done" {
		t.Fatalf("provision вызван %d раз, outputs %v, шаг %+v", calls, job.Outputs, job.Steps[0])
	}
}

// Шаг с поиском по имени (postgres): прерванный create без id в ответе —
// следующий crank находит кластер через pg_list и не создаёт второй.
func TestCancel_StackStepInterruptedReconcilesByName(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"i","postgres":{"name":"p"}}`)
	ctx, cancel := context.WithCancel(context.Background())
	creates := 0
	r.handlers["pg_create"] = func(context.Context, map[string]any) (string, bool) {
		creates++
		cancel()
		return errResult(context.Canceled), true
	}
	// Кластер появляется в списке только после create (до него имя
	// свободно: занятое имя до первого create — чужой ресурс).
	r.handlers["pg_list"] = func(context.Context, map[string]any) (string, bool) {
		if creates == 0 {
			return toJSON(map[string]any{"clusters": []any{}}), false
		}
		return toJSON(map[string]any{"clusters": []any{map[string]any{"name": "p", "id": "pg-1"}}}), false
	}
	r.handlers["pg_get"] = fakeOK(map[string]any{"status": "CREATING"})
	r.handleStackApply(ctx, map[string]any{"job_id": j.ID, "confirmed": true})
	r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	job, _ := stack.LoadJob(j.ID)
	if creates != 1 || job.Steps[0].ResourceID != "pg-1" || job.Steps[0].Status != "waiting" {
		t.Fatalf("pg_create вызван %d раз, шаг %+v", creates, job.Steps[0])
	}
}

// Квота публичных IP исчерпана: ВМ без интернета не создаётся вовсе.
func TestProvision_FIPQuota_CreatesNothing(t *testing.T) {
	cloud := &cancelCloud{cancel: func() {}, fipErr: errors.New("403 quota exceeded")}
	r, _ := cancelRegistry(t, cloud)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !isErr || !strings.Contains(text, "quota") {
		t.Fatalf("ответ: %s", text)
	}
	if cloud.creates != 0 {
		t.Errorf("ВМ создана без публичного IP")
	}
}

// State не пишется: ответ говорит, что ВМ в state нет и destroy её не найдёт.
func TestProvision_StateNotWritable_Warns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &cancelCloud{cancel: cancel}
	r, dir := cancelRegistry(t, cloud)
	if err := os.MkdirAll(filepath.Dir(state.DefaultPath(dir)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.DefaultPath(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true})
	if !isErr || !strings.Contains(text, "vm-made") || !strings.Contains(text, "NOT in the state") {
		t.Fatalf("ответ не предупреждает о state: %s", text)
	}
}

// errorStateCloud creates a VM that goes to a failed status ("error" by
// default) on the first poll.
type errorStateCloud struct {
	cancelCloud
	status string
}

func (c *errorStateCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	c.createReq = req
	return &provision.VM{ID: "vm-err"}, nil
}
func (c *errorStateCloud) GetVM(context.Context, string) (*provision.VM, error) {
	if c.status != "" {
		return &provision.VM{ID: "vm-err", Status: c.status}, nil
	}
	return &provision.VM{ID: "vm-err", Status: "error"}, nil
}

// A VM that goes to "error" is billed with its boot FIP: provision records
// both and names them, instead of a bare error that left them unknown to
// destroy (QA H3). "error_creating" is what ru.AZ-1 answered after 19 min
// in creating (cloud-e2e 15.10.2026).
func TestProvision_ErrorStateRecordsVMAndFIP(t *testing.T) {
	for _, status := range []string{"error", "error_creating"} {
		t.Run(status, func(t *testing.T) { testProvisionErrorState(t, status) })
	}
}

func testProvisionErrorState(t *testing.T, status string) {
	cloud := &errorStateCloud{status: status}
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
			Timings:   engine.Timings{VMPollInterval: time.Millisecond},
		}, nil
	})
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !isErr || !strings.Contains(text, "vm-err") || !strings.Contains(text, "fip-boot") || !strings.Contains(text, "destroy") {
		t.Fatalf("answer must name the VM, its FIP and destroy: %s", text)
	}
	rec := stateHasVM(t, dir, "vm-err")
	if rec.Status != "error" || rec.FloatingIPID != "fip-boot" {
		t.Errorf("record = %+v, want status error with floating_ip_id fip-boot", rec)
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (*cancelCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}

// errNoPriceAPI — ответ подделок на PriceCalculation: расчёт цены не
// получен, превью и план показывают оценку по карте, как раньше.
var errNoPriceAPI = errors.New("price-calculation: not in this fake")

// ListFloatingIPs: provision first looks for a boot address left by an
// earlier run; there is none.
func (c *cancelCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}
