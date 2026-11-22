// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// oneDiskCloud is the preview world with one disk, data.
type oneDiskCloud struct{ previewCloud }

func (oneDiskCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	return []map[string]any{{"id": "00000000-0000-4000-8000-0000000000d1", "name": "data", "state": "available"}}, nil
}

// complete runs what the shell runs on Tab (cobra's __complete) in the
// preview world with a key: the candidates, one per line, and the
// directive.
func complete(t *testing.T, args ...string) string {
	t.Helper()
	withCloud(t, oneDiskCloud{})
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	t.Setenv("CLOUDRU_KEY_ID", "00000000000000000000000000000000")
	t.Setenv("CLOUDRU_SECRET", "00000000000000000000000000000000")
	root := newRootCmd()
	root.SetArgs(append([]string{"__complete"}, args...))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	return out.String()
}

// Release plan 1.5, Tab: a flag of a tree command, a positional of one, an
// engine command's flag and the root's --region complete from the same
// declaration of the registry; a flag that names a file keeps the shell's
// file completion.
func TestTab_CompletesReferencesEverywhere(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"tree flag":     {[]string{"vm", "create", "web", "--flavor", "lowcost10-2"}, "lowcost10-2-4"},
		"engine flag":   {[]string{"deploy", "--region", "ru."}, "ru.AZ-1"},
		"root flag":     {[]string{"vm", "zones", "--region", ""}, "ru.AZ-1"},
		"narrowed away": {[]string{"vm", "create", "web", "--flavor", "free"}, "free-tier1"},
		"positional":    {[]string{"disk", "delete", ""}, "00000000-0000-4000-8000-0000000000d1\tdata"},
	} {
		t.Run(name, func(t *testing.T) {
			if out := complete(t, tc.args...); !strings.Contains(out, tc.want) || !strings.Contains(out, ":4") {
				t.Errorf("Tab on %v: want %s and no file completion (:4), got\n%s", tc.args, tc.want, out)
			}
		})
	}
	if out := complete(t, "vm", "create", "web", "--flavor", "lowcost10-2"); strings.Contains(out, "free-tier1") {
		t.Errorf("--flavor lowcost10-2+Tab offers what does not start so:\n%s", out)
	}
	// deploy --compose-file names a file: no completion of evo's own.
	if out := complete(t, "deploy", "--compose-file", ""); strings.Contains(out, ":4") {
		t.Errorf("--compose-file lost the shell's file completion:\n%s", out)
	}
}

func (oneDiskCloud) ListInterfaces(_ context.Context, project string) ([]*provision.Interface, error) {
	if project != pvProject {
		return nil, nil
	}
	return []*provision.Interface{
		{ID: "00000000-0000-4000-8000-00000000a111", VM: &provision.InterfaceVM{ID: parityVM}},
		{ID: "00000000-0000-4000-8000-00000000a222", VM: &provision.InterfaceVM{ID: "00000000-0000-4000-8000-00000000b222"}},
	}, nil
}

func TestTab_UsesOnlySuppliedVMForInterface(t *testing.T) {
	for _, tc := range []struct {
		cmd         []string
		parent, arg string
	}{
		{[]string{"tg", "create", "web"}, "--vm-id", "--interface-id"},
		{[]string{"vpc", "route-create", pvProject}, "--next-hop-vm-id", "--next-hop-interface-id"},
	} {
		for _, selected := range []bool{false, true} {
			args := slices.Clone(tc.cmd)
			if selected {
				args = append(args, tc.parent, parityVM)
			}
			out := complete(t, append(args, tc.arg, "")...)
			if strings.Contains(out, "00000000-0000-4000-8000-00000000a111") != selected || strings.Contains(out, "00000000-0000-4000-8000-00000000a222") || !strings.Contains(out, ":4") {
				t.Fatalf("%v selected=%t: %s", tc.cmd, selected, out)
			}
		}
	}
}
