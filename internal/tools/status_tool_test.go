// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// statusCloud answers GET /vms/{id} with get and counts the calls.
type statusCloud struct {
	provision.CloudAPI
	get   func() (*provision.VM, error)
	calls int
}

func (*statusCloud) SetToken(string) {}
func (c *statusCloud) GetVM(context.Context, string) (*provision.VM, error) {
	c.calls++
	return c.get()
}

// The status tool asks the cloud by default and reports it in cloud_*
// fields next to the record; refresh=false reads the state only. It never
// writes the state file.
func TestStatusTool_Refresh(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      map[string]any
		get       func() (*provision.VM, error)
		wantCalls int
		check     func(map[string]any) bool
	}{
		{"live", nil, func() (*provision.VM, error) { return &provision.VM{ID: "vm-1", Status: "running"}, nil }, 1,
			func(m map[string]any) bool { return m["cloud_checked"] == true && m["cloud_state"] == "running" }},
		{"404", nil, func() (*provision.VM, error) {
			return nil, clerrors.New(clerrors.CodeVMNotFound, "resource not found", "")
		}, 1,
			func(m map[string]any) bool { return m["cloud_checked"] == true && m["not_found_in_cloud"] == true }},
		{"error", nil, func() (*provision.VM, error) {
			return nil, clerrors.New(clerrors.CodeAPIError, "API returned status 500", "")
		}, 1,
			func(m map[string]any) bool {
				e, _ := m["cloud_error"].(string)
				return m["cloud_checked"] == false && m["cloud_reason"] == clerrors.CodeAPIError && strings.Contains(e, "status 500")
			}},
		{"refresh=false", map[string]any{"refresh": false}, func() (*provision.VM, error) { panic("no GET with refresh=false") }, 0,
			func(m map[string]any) bool { return m["cloud_checked"] == false && m["cloud_error"] == nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cloud := &statusCloud{get: tt.get}
			r, dir := targetRegistry(t, cloud)
			before, err := os.ReadFile(state.DefaultPath(dir))
			if err != nil {
				t.Fatal(err)
			}
			args := tt.args
			if args == nil {
				args = map[string]any{}
			}
			got := callTool(t, r.handlers["status"], args)
			if got["vm_id"] != "vm-1" || got["status"] != "running" {
				t.Errorf("record fields lost: %v", got)
			}
			if !tt.check(got) {
				t.Errorf("answer: %v", got)
			}
			if cloud.calls != tt.wantCalls {
				t.Errorf("GET calls = %d, want %d", cloud.calls, tt.wantCalls)
			}
			after, _ := os.ReadFile(state.DefaultPath(dir))
			if !bytes.Equal(before, after) {
				t.Error("status rewrote the state file")
			}
		})
	}
}
