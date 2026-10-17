package provision

import (
	"context"
	"errors"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

type listVMsCloud struct {
	CloudAPI
	vms []*VM
	err error
}

func (c *listVMsCloud) ListVMsInProject(context.Context, string) ([]*VM, error) {
	return c.vms, c.err
}

func TestFindCreatedVM(t *testing.T) {
	since := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		vms    []*VM
		err    error
		wantID string
	}{
		{"made by this call", []*VM{{ID: "vm-new", Name: "evo-app", CreatedAt: since.Add(10 * time.Second)}}, nil, "vm-new"},
		{"clock a little behind", []*VM{{ID: "vm-new", Name: "evo-app", CreatedAt: since.Add(-time.Minute)}}, nil, "vm-new"},
		{"older VM with the same name", []*VM{{ID: "vm-old", Name: "evo-app", CreatedAt: since.Add(-time.Hour)}}, nil, ""},
		{"no creation time", []*VM{{ID: "vm-x", Name: "evo-app"}}, nil, ""},
		{"other name", []*VM{{ID: "vm-y", Name: "evo-other", CreatedAt: since}}, nil, ""},
		{"lookup fails", nil, errors.New("503"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := FindCreatedVM(&listVMsCloud{vms: tc.vms, err: tc.err}, "p", "evo-app", since)
			got := ""
			if vm != nil {
				got = vm.ID
			}
			if got != tc.wantID {
				t.Errorf("FindCreatedVM = %q, want %q", got, tc.wantID)
			}
		})
	}
}

// Отказ API (4xx, лимит запросов) — create ничего не сделал; обрыв, отмена
// и 5xx — не отказ: ВМ могла быть создана (release audit, L-01).
func TestCreateRejected(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{clierrors.Wrap(clierrors.CodeAPIError, "409", "", &StatusError{Status: 409}), true},
		{clierrors.Wrap(clierrors.CodeAPIError, "422", "", &StatusError{Status: 422}), true},
		{clierrors.Wrap(clierrors.CodeAuthFailed, "403", "", &StatusError{Status: 403}), true},
		{clierrors.New(clierrors.CodeAPIRateLimited, "429", ""), true},
		{clierrors.Wrap(clierrors.CodeAPIError, "500", "", &StatusError{Status: 500}), false},
		{clierrors.Wrap(clierrors.CodeAPIUnreachable, "reset", "", errors.New("connection reset")), false},
		{context.DeadlineExceeded, false},
	} {
		if got := CreateRejected(tc.err); got != tc.want {
			t.Errorf("CreateRejected(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
	if !IsNameTaken(clierrors.Wrap(clierrors.CodeAPIError, "409", "", &StatusError{Status: 409})) {
		t.Error("409 is not a taken name")
	}
}
