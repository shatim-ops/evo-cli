package provision

import (
	"context"
	"errors"
	"net/http"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
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
