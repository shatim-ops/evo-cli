package provision

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Релизный аудит, R-06: списки compute, которым API требует project_id
// (ВМ, подсети, группы, floating IP, диски, NAT), без проекта не уходят в
// облако и отвечают CONFIG_INVALID — не сырой 422 с кодом 5.
func TestProjectScopedListsNeedAProject(t *testing.T) {
	var calls atomic.Int32
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `[{"message":"Field required","code":"missing","field":"query.project_id"}]`, http.StatusUnprocessableEntity)
	})
	defer srv.Close()
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"vms":          func() error { _, err := a.ListVMsInProject(ctx, ""); return err },
		"subnets":      func() error { _, err := a.ListSubnets(ctx, ""); return err },
		"groups":       func() error { _, err := a.ListSecurityGroups(ctx, ""); return err },
		"floating IPs": func() error { _, err := a.ListFloatingIPs(ctx, ""); return err },
		"disks":        func() error { _, err := a.ListDisks(ctx, ""); return err },
		"NAT gateways": func() error { _, err := a.ListNATGateways(ctx, ""); return err },
	} {
		var ce *clierrors.CLIError
		if err := call(); !errors.As(err, &ce) || ce.Code != clierrors.CodeConfigInvalid || ce.Message != clierrors.ProjectRequired().Message {
			t.Errorf("%s without a project: %v", name, err)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("%d requests went out without a project", n)
	}
	// Типы дисков проект не требуют: запрос идёт и без него.
	_, _ = a.ListDiskTypes(ctx, "")
	if calls.Load() != 1 {
		t.Errorf("disk types without a project: %d requests", calls.Load())
	}
}
