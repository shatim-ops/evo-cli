// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"errors"
	"net/http"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// createdVMLookupTimeout bounds FindCreatedVM: it runs after a failed or
// cancelled create, often with the caller's context already done.
const createdVMLookupTimeout = 30 * time.Second

// createdVMClockSkew is how much earlier than the create call a VM's
// created_time may read and still count as made by that call.
const createdVMClockSkew = 5 * time.Minute

// FindCreatedVM looks for the VM a CreateVM call may have made although it
// returned an error: the platform can create the VM and the answer be lost
// (timeout, Ctrl-C, a dropped connection). The VM is matched by name and by
// a creation time no earlier than since (less clock skew), so an older VM
// that happens to share the name is not taken for it. It uses its own
// context and returns nil when nothing matches or the lookup fails.
func FindCreatedVM(cloud CloudAPI, projectID, name string, since time.Time) *VM {
	ctx, cancel := context.WithTimeout(context.Background(), createdVMLookupTimeout)
	defer cancel()
	vms, err := cloud.ListVMsInProject(ctx, projectID)
	if err != nil {
		return nil
	}
	for _, vm := range vms {
		if vm == nil || vm.ID == "" || vm.Name != name || vm.CreatedAt.IsZero() {
			continue
		}
		if vm.CreatedAt.After(since.Add(-createdVMClockSkew)) {
			return vm
		}
	}
	return nil
}

// CreateVMChecked is cloud.CreateVM whose success must name the VM. An
// answer without an id (POST /vms answered [{}], break r3a R3A-1) is taken
// as a lost answer, not a VM: the VM may exist, so the caller looks it up
// by name (FindCreatedVM) instead of recording an empty id that neither
// status nor destroy can follow.
func CreateVMChecked(ctx context.Context, cloud CloudAPI, req CreateVMRequest) (*VM, error) {
	vm, err := cloud.CreateVM(ctx, req)
	if err == nil && (vm == nil || vm.ID == "") {
		return nil, clierrors.New(clierrors.CodeAPIError,
			i18n.T("Cloud.ru accepted the VM create, but the answer has no VM id",
				"Cloud.ru принял создание ВМ, но в ответе нет её id"),
			i18n.T("Check the VM list ('evo vm list'): the VM may have been created.",
				"Проверьте список ВМ ('evo vm list'): ВМ могла быть создана."))
	}
	return vm, err
}

// CreateRejected reports whether a failed create was answered by the API
// with a refusal (HTTP 4xx: 409 vm_with_this_data_already_exists, 422, a
// rate limit, …): then this call made nothing, and a VM FindCreatedVM
// would find by the name is another one — taking it for the new VM left
// the retry's boot FIP billed and overwrote that VM's record (release
// audit, L-01). A lost answer, a cancel or a 5xx is not a refusal.
func CreateRejected(err error) bool {
	if st := HTTPStatus(err); st >= 400 && st < 500 {
		return true
	}
	var ce *clierrors.CLIError
	return errors.As(err, &ce) && (ce.Code == clierrors.CodeAPIRateLimited || ce.Code == clierrors.CodeVMNotFound)
}

// IsNameTaken reports the 409 of a create whose name is already used in
// the project (vm_with_this_data_already_exists).
func IsNameTaken(err error) bool { return HTTPStatus(err) == http.StatusConflict }
