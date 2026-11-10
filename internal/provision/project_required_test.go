// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"errors"
	"net/http"
	"strings"
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

// Release plan 1.8, L3 («Риск», conclusion 43, (3)): a list of a project
// the key does not see answers 404 — the refusal names that, not «resource
// not found: GET /api/v1/vms», and keeps its code (VM_NOT_FOUND for VMs,
// NOT_FOUND else).
func TestProjectScopedLists404NamesTheProject(t *testing.T) {
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"not_found"}`, http.StatusNotFound)
	})
	defer srv.Close()
	ctx := context.Background()
	for name, c := range map[string]struct {
		call func() error
		code string
	}{
		"vms":     {func() error { _, err := a.ListVMsInProject(ctx, "p-1"); return err }, clierrors.CodeVMNotFound},
		"subnets": {func() error { _, err := a.ListSubnets(ctx, "p-1"); return err }, clierrors.CodeNotFound},
		"disks":   {func() error { _, err := a.ListDisks(ctx, "p-1"); return err }, clierrors.CodeNotFound},
	} {
		var ce *clierrors.CLIError
		if err := c.call(); !errors.As(err, &ce) || ce.Code != c.code || !strings.Contains(ce.Message, "p-1") ||
			!strings.Contains(ce.Message, "not visible to this key") || !strings.Contains(ce.Hint, "evo whoami") {
			t.Errorf("%s of a project the key does not see: %v", name, err)
		}
	}
}
