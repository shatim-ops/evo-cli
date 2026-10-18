// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// statusEngine: an engine with one running deployment in its state and
// separate stdout/stderr buffers. It returns the state file's bytes, to
// check that status never rewrites it.
func statusEngine(t *testing.T, cloud *mockCloudAPI, mode output.Mode) (eng *Engine, out, errOut *bytes.Buffer, stateBytes func() []byte) {
	t.Helper()
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	out, errOut = &bytes.Buffer{}, &bytes.Buffer{}
	eng.Printer = output.NewWithWriters(out, errOut, mode, false)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "dep-1", VMID: "vm-1", VMIP: "203.0.113.5", Status: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now()})
	path := state.DefaultPath(dir)
	if err := state.Write(path, sf); err != nil {
		t.Fatal(err)
	}
	return eng, out, errOut, func() []byte {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
}

// The VM is there: status shows its live state next to the local one.
func TestStatus_LiveState(t *testing.T) {
	cloud := &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
		return &provision.VM{ID: id, Status: "stopped"}, nil
	}}
	eng, out, _, stateBytes := statusEngine(t, cloud, output.ModePlain)
	before := stateBytes()
	if err := eng.Status(context.Background(), StatusOptions{}); err != nil {
		t.Fatal(err)
	}
	if cloud.getCalls != 1 {
		t.Errorf("GET calls = %d, want 1", cloud.getCalls)
	}
	if !strings.Contains(out.String(), "In the cloud") || !strings.Contains(out.String(), "stopped") {
		t.Errorf("no live state in the table:\n%s", out)
	}
	if cloud.token != "test-token" {
		t.Errorf("GET without a token: %q", cloud.token)
	}
	if !bytes.Equal(before, stateBytes()) {
		t.Error("status rewrote the state file")
	}
}

// Deleted in the console: 404 shows as "not found", with 'evo destroy' as
// the way to clean up; the record itself is left alone.
func TestStatus_NotFoundInCloud(t *testing.T) {
	cloud := &mockCloudAPI{getVMFn: func(context.Context, string) (*provision.VM, error) {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "resource not found", "")
	}}
	eng, out, errOut, stateBytes := statusEngine(t, cloud, output.ModePlain)
	before := stateBytes()
	if err := eng.Status(context.Background(), StatusOptions{}); err != nil {
		t.Fatalf("status must not fail on 404: %v", err)
	}
	if !strings.Contains(out.String(), "not found (deleted outside evo) → evo destroy") {
		t.Errorf("no 'not found' in the table:\n%s", out)
	}
	if !strings.Contains(out.String(), "running") {
		t.Errorf("the local state is not shown:\n%s", out)
	}
	if !strings.Contains(errOut.String(), "evo destroy") {
		t.Errorf("no hint to destroy: %q", errOut)
	}
	if !bytes.Equal(before, stateBytes()) {
		t.Error("status rewrote the state file")
	}
}

// Any other failure (5xx, network, no key) does not fail status: the local
// state is shown with the reason the cloud was not checked.
func TestStatus_CloudErrorKeepsLocalState(t *testing.T) {
	cloud := &mockCloudAPI{getVMFn: func(context.Context, string) (*provision.VM, error) {
		return nil, clerrors.New(clerrors.CodeAPIError, "API returned status 503", "retry later")
	}}
	eng, out, _, _ := statusEngine(t, cloud, output.ModePlain)
	if err := eng.Status(context.Background(), StatusOptions{}); err != nil {
		t.Fatalf("status must not fail on an API error: %v", err)
	}
	if !strings.Contains(out.String(), "not checked: API returned status 503") || !strings.Contains(out.String(), "running") {
		t.Errorf("table:\n%s", out)
	}

	// No key: not even a GET.
	cloud = &mockCloudAPI{}
	eng, out, _, _ = statusEngine(t, cloud, output.ModePlain)
	eng.Auth = &mockTokenProvider{err: clerrors.New(clerrors.CodeAuthRequired, "Cloud.ru credentials not set", "")}
	if err := eng.Status(context.Background(), StatusOptions{}); err != nil {
		t.Fatal(err)
	}
	if cloud.getCalls != 0 || !strings.Contains(out.String(), "not checked: Cloud.ru credentials not set") {
		t.Errorf("GET calls %d, table:\n%s", cloud.getCalls, out)
	}
}

// A cloud that does not answer: status gives up after StatusRefreshTimeout.
func TestStatus_RefreshIsBounded(t *testing.T) {
	cloud := &mockCloudAPI{getVMFn: func(ctx context.Context, _ string) (*provision.VM, error) {
		<-ctx.Done()
		return nil, clerrors.Wrap(clerrors.CodeAPIUnreachable, "request cancelled", "", ctx.Err())
	}}
	eng, out, _, _ := statusEngine(t, cloud, output.ModePlain)
	eng.Timings.StatusRefreshTimeout = 20 * time.Millisecond
	start := time.Now()
	if err := eng.Status(context.Background(), StatusOptions{}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("status took %s", d)
	}
	if !strings.Contains(out.String(), "not checked: no answer within 20ms") {
		t.Errorf("table:\n%s", out)
	}
}

// --no-refresh: the local state only, no request.
func TestStatus_NoRefresh(t *testing.T) {
	cloud := &mockCloudAPI{getVMFn: func(context.Context, string) (*provision.VM, error) {
		return nil, errors.New("must not be called")
	}}
	eng, out, _, _ := statusEngine(t, cloud, output.ModePlain)
	eng.Auth = &mockTokenProvider{err: errors.New("must not be called")}
	if err := eng.Status(context.Background(), StatusOptions{NoRefresh: true}); err != nil {
		t.Fatal(err)
	}
	if cloud.getCalls != 0 {
		t.Errorf("GET calls = %d, want 0", cloud.getCalls)
	}
	if strings.Contains(out.String(), "In the cloud") || !strings.Contains(out.String(), "running") {
		t.Errorf("table:\n%s", out)
	}
}

// --json: the record as before plus the cloud_* fields.
func TestStatus_JSONCloudFields(t *testing.T) {
	for _, tt := range []struct {
		name  string
		get   func(context.Context, string) (*provision.VM, error)
		opts  StatusOptions
		check func(t *testing.T, got map[string]any)
	}{
		{"live", func(_ context.Context, id string) (*provision.VM, error) {
			return &provision.VM{ID: id, Status: "running"}, nil
		},
			StatusOptions{}, func(t *testing.T, got map[string]any) {
				if got["cloud_checked"] != true || got["cloud_state"] != "running" || got["not_found_in_cloud"] != nil {
					t.Errorf("%v", got)
				}
			}},
		{"404", func(context.Context, string) (*provision.VM, error) {
			return nil, clerrors.New(clerrors.CodeVMNotFound, "resource not found", "")
		}, StatusOptions{}, func(t *testing.T, got map[string]any) {
			if got["cloud_checked"] != true || got["not_found_in_cloud"] != true {
				t.Errorf("%v", got)
			}
		}},
		{"error", func(context.Context, string) (*provision.VM, error) {
			return nil, clerrors.New(clerrors.CodeAPIUnreachable, "cannot reach Cloud.ru API", "")
		}, StatusOptions{}, func(t *testing.T, got map[string]any) {
			if got["cloud_checked"] != false || got["cloud_reason"] != clerrors.CodeAPIUnreachable ||
				got["cloud_error"] != "cannot reach Cloud.ru API" {
				t.Errorf("%v", got)
			}
		}},
		{"no-refresh", nil, StatusOptions{NoRefresh: true}, func(t *testing.T, got map[string]any) {
			if got["cloud_checked"] != false || got["cloud_error"] != nil {
				t.Errorf("%v", got)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cloud := &mockCloudAPI{getVMFn: tt.get}
			eng, out, _, _ := statusEngine(t, cloud, output.ModeJSON)
			if err := eng.Status(context.Background(), tt.opts); err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, out)
			}
			if got["vm_id"] != "vm-1" || got["status"] != "running" {
				t.Errorf("record fields lost: %v", got)
			}
			tt.check(t, got)
		})
	}
}

// Запись другого проекта: GET /vms/{id} проекта не несёт, и его 404 не
// значит «удалена» — облако не спрашивается (ревью Fable на #81).
func TestStatus_RecordOfAnotherProject(t *testing.T) {
	cloud := &mockCloudAPI{getVMFn: func(context.Context, string) (*provision.VM, error) {
		t.Error("GET for a record of another project")
		return nil, clerrors.New(clerrors.CodeVMNotFound, "resource not found", "")
	}}
	eng, out, _, _ := statusEngine(t, cloud, output.ModePlain)
	eng.Config.ProjectID = "p-active"
	rec := &state.DeploymentRecord{VMID: "vm-1", ProjectID: "p-other"}
	c := eng.CheckCloud(context.Background(), rec)
	if c.Checked || c.NotFound || c.Reason != clerrors.CodeConfigInvalid || !strings.Contains(c.Error, "another project") {
		t.Errorf("check: %+v (%s)", c, out)
	}
}
