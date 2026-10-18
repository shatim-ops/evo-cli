// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// conflictCloud: ВМ с именем плана появилась между проверкой и create
// (другой вызов), и create отвечает 409.
type conflictCloud struct {
	provision.CloudAPI
	listed int
}

func (*conflictCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	return nil, clierrors.Wrap(clierrors.CodeAPIError, "API returned status 409", "",
		&provision.StatusError{Status: 409, Body: `{"code":"vm_with_this_data_already_exists"}`})
}
func (c *conflictCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	c.listed++
	if c.listed == 1 {
		return nil, nil // the idempotency check before the create
	}
	return []*provision.VM{{ID: "vm-other", Name: "x", CreatedAt: time.Now()}}, nil
}

// Внешний аудит релиза v1.0.0, L-01: после отказа API (409) deploy_apply не
// выдаёт найденную по имени ВМ за созданную.
func TestAudit_Apply_RejectedCreateDoesNotNameAnotherVM(t *testing.T) {
	res, err := ApplyWithTimeout(context.Background(), &conflictCloud{}, &Plan{ProjectID: "p", VM: VMSpec{Name: "x"}}, 0)
	if err == nil || provision.HTTPStatus(err) != 409 {
		t.Fatalf("Apply = %v, want the 409", err)
	}
	if res != nil {
		t.Errorf("result %+v, want none: vm-other is not this call's VM", res)
	}
}
