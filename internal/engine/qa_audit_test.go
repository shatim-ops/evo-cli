package engine

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Внешний аудит релиза v1.0.0 (#90).

// L-01: create ответил 409 (имя занято), а ВМ с этим именем создана только
// что — другим вызовом. deploy её не усыновляет и не пишет в state,
// загрузочный FIP освобождает, ошибка — CONFIG_INVALID с подсказкой.
func TestAudit_Deploy_NameTaken409DoesNotAdopt(t *testing.T) {
	cloud := &mockCloudAPI{
		createVMFn: func(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
			return nil, clerrors.Wrap(clerrors.CodeAPIError, "API returned status 409", "",
				&provision.StatusError{Status: 409, Body: `{"code":"vm_with_this_data_already_exists"}`})
		},
	}
	cloud.listVMsInProjectFn = func(context.Context, string) ([]*provision.VM, error) {
		return []*provision.VM{{ID: "vm-other", Name: cloud.createReq.Name, CreatedAt: time.Now()}}, nil
	}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())

	err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) || ce.Code != clerrors.CodeConfigInvalid || provision.HTTPStatus(err) != 409 {
		t.Fatalf("deploy error %v, want CONFIG_INVALID over the 409", err)
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-1" {
		t.Errorf("released FIPs %v, want the boot FIP fip-1", cloud.deletedFIPs)
	}
	if a := readActive(t, dir); a != nil {
		t.Errorf("state records %+v, want nothing: the VM is not this deploy's", a)
	}
}

// A-005: после неподтверждённого удаления повтор destroy, когда ВМ уже
// нет, освобождает FIP и снимает запись.
func TestAudit_Destroy_RerunAfterUnconfirmedFinishes(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, runningRecord())
	cloud.getVMFn = func(_ context.Context, id string) (*provision.VM, error) {
		return &provision.VM{ID: id, Status: "deleting"}, nil
	}
	eng.Timings.DestroyPollTimeout = 20 * time.Millisecond
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); !stderrors.Is(err, ErrDeletionUnconfirmed) {
		t.Fatalf("first destroy = %v, want the unconfirmed deletion", err)
	}
	if len(cloud.deletedFIPs) != 0 {
		t.Fatalf("FIP released before the VM was gone: %v", cloud.deletedFIPs)
	}

	notFound := func() error { return clerrors.New(clerrors.CodeVMNotFound, "resource not found", "") }
	cloud.getVMFn = func(context.Context, string) (*provision.VM, error) { return nil, notFound() }
	cloud.deleteVMFn = func(context.Context, string) error { return notFound() }
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-9" {
		t.Errorf("released FIPs %v, want fip-9", cloud.deletedFIPs)
	}
	if a := readActive(t, eng.StateDir); a != nil {
		t.Errorf("record left: %+v", a)
	}
}
