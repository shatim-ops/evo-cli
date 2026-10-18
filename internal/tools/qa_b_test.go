// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Релизный QA, B-lifecycle: учёт floating IP у deploy_apply и destroy.

func qaBRegistry(t *testing.T, cloud provision.CloudAPI, dir string) *Registry {
	t.Helper()
	return New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{
			Config: config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
			Auth:   staticToken{}, Cloud: cloud,
			Printer:  output.NewWithWriter(&bytes.Buffer{}, output.ModeText, false),
			StateDir: dir, SSHKeyPEM: []byte("key"), SSHPubKey: "ssh-ed25519 AAAA test",
			Timings: engine.Timings{FIPRetryInterval: time.Millisecond, DestroyPollInterval: time.Millisecond, DestroyPollTimeout: time.Second},
		}, nil
	})
}

// №2: повтор deploy_apply, не прочитавший интерфейс ВМ, не затирает
// floating IP старой записи — иначе destroy его не нашёл бы.
func TestQAB_RecordDeployment_KeepsFloatingIPOfOldRecord(t *testing.T) {
	dir := t.TempDir()
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "203.0.113.5", FloatingIPID: "fip-1", Status: "creating"})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	res := &deployplan.ApplyResult{VMID: "vm-1", Status: "creating"}
	if err := recordDeployment(dir, &deployplan.Plan{ProjectID: "p", VM: deployplan.VMSpec{Name: "x"}}, res, res.Status); err != nil {
		t.Fatal(err)
	}
	got, _ := state.Read(state.DefaultPath(dir))
	if rec := got.FindDeployment("vm-1"); rec.FloatingIPID != "fip-1" || rec.VMIP != "203.0.113.5" {
		t.Fatalf("floating_ip_id=%q vm_ip=%q", rec.FloatingIPID, rec.VMIP)
	}
}

// ВМ, которую этот каталог не записывал, с адресом на ней.
type foreignVMCloud struct {
	provision.CloudAPI
	fipName  string
	gone     bool
	released string
}

func (*foreignVMCloud) SetToken(string) {}
func (*foreignVMCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: "vm-9", Name: "user-vm", FloatingIP: "198.51.100.7"}}, nil
}
func (c *foreignVMCloud) DeleteVMInProject(context.Context, string, string) error {
	c.gone = true
	return nil
}
func (c *foreignVMCloud) GetVM(context.Context, string) (*provision.VM, error) {
	if c.gone {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	return &provision.VM{ID: "vm-9"}, nil
}
func (c *foreignVMCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: "fip-user", Name: c.fipName, IPAddress: "198.51.100.7", State: "in_use"}}, nil
}
func (c *foreignVMCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.released = id
	return nil
}

// №5: destroy vm_id чужой ВМ не удаляет адрес, зарезервированный
// пользователем; адрес evo (deploy_apply «<имя>-fip») — удаляет; явный
// release_ip=true — удаляет любой. Превью говорит о правиле.
func TestQAB_DestroyTargeted_ForeignVMFloatingIP(t *testing.T) {
	for _, tc := range []struct {
		fipName string
		release any
		want    string
	}{
		{"my-static-ip", nil, ""},
		{"user-vm-fip", nil, "fip-user"},
		{"my-static-ip", true, "fip-user"},
	} {
		cloud := &foreignVMCloud{fipName: tc.fipName}
		r := qaBRegistry(t, cloud, t.TempDir())
		preview, _ := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-9"})
		if !strings.Contains(preview, "release_ip=true") {
			t.Fatalf("превью не говорит о floating IP: %s", preview)
		}
		args := map[string]any{"vm_id": "vm-9", "confirmed": true}
		if tc.release != nil {
			args["release_ip"] = tc.release
		}
		text, isErr := r.handlers["destroy"](context.Background(), args)
		if isErr || cloud.released != tc.want {
			t.Errorf("%s release_ip=%v: освобождён %q, ждали %q; ответ %s", tc.fipName, tc.release, cloud.released, tc.want, text)
		}
		if tc.want == "" && !strings.Contains(text, "kept") {
			t.Errorf("ответ не говорит, что адрес оставлен: %s", text)
		}
	}
}

// Ревью Grok по #87: ВМ не дошла до running к сроку — пароль, созданный
// для неё и нигде не сохранённый, приходит в этом ответе (повтор подхватит
// ВМ без него), план остаётся.
type slowVMCloud struct {
	provision.CloudAPI
}

func (*slowVMCloud) SetToken(string) {}
func (*slowVMCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return nil, nil
}
func (*slowVMCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	return &provision.VM{ID: "vm-slow", Status: "creating"}, nil
}
func (*slowVMCloud) GetVM(_ context.Context, id string) (*provision.VM, error) {
	return &provision.VM{ID: id, Status: "creating"}, nil
}

func TestQAB_DeployApply_TimeoutGivesPassword(t *testing.T) {
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{
			Config: config.Config{ProjectID: "p"}, Auth: staticToken{}, Cloud: &slowVMCloud{},
			Printer:  output.NewWithWriter(&bytes.Buffer{}, output.ModeText, false),
			StateDir: dir, Timings: engine.Timings{VMPollTimeout: 20 * time.Millisecond},
		}, nil
	})
	plan := &deployplan.Plan{PlanID: "plan-0123456789ab", ExpiresAt: time.Now().Add(time.Hour), ProjectID: "p",
		VM: deployplan.VMSpec{Name: "win-1"}, MetaFields: []string{"plain_password"}}
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": plan.PlanID, "confirmed": true})
	if !isErr || !strings.Contains(text, `"password"`) || !strings.Contains(text, "give it to the user now") {
		t.Fatalf("ответ без пароля: %s", text)
	}
	if _, err := deployplan.Load(dir, plan.PlanID); err != nil {
		t.Errorf("план удалён при таймауте: %v", err)
	}
}

// Ревью Grok по #87: подхваченная остановленная ВМ сама не запустится —
// подсказка «запустите в консоли», а не «повторите позже».
func TestQAB_DeployApply_StoppedVMSaysStartIt(t *testing.T) {
	cloud := &sgCloud{vm: &provision.VM{ID: "vm-1", Name: "wiki-1", Status: "stopped"}}
	r, dir := sgRegistry(t, cloud)
	plan := &deployplan.Plan{PlanID: "plan-0123456789ab", ExpiresAt: time.Now().Add(time.Hour), Product: "wiki",
		VM: deployplan.VMSpec{Name: "wiki-1"}, Network: deployplan.NetworkSpec{SecurityGroupID: "sg-1"}}
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": plan.PlanID, "confirmed": true})
	if !isErr || !strings.Contains(text, "start it in the Cloud.ru console") || strings.Contains(text, "in a few minutes") {
		t.Fatalf("ответ для остановленной ВМ: %s", text)
	}
}

// Ревью Grok по #87: список floating IP не прочитан — адрес оставлен, и
// ответ говорит, что проверить не удалось, а не «evo его не создавал».
type fipListFailsCloud struct{ foreignVMCloud }

func (*fipListFailsCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, clerrors.New(clerrors.CodeAPIError, "502 bad gateway", "")
}

func TestQAB_DestroyTargeted_FIPListFailsSaysSo(t *testing.T) {
	cloud := &fipListFailsCloud{}
	r := qaBRegistry(t, cloud, t.TempDir())
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-9", "confirmed": true})
	if isErr || cloud.released != "" || !strings.Contains(text, "could not check who made it") {
		t.Fatalf("освобождён %q; ответ %s", cloud.released, text)
	}
}

func (*sgCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{}, nil
}
