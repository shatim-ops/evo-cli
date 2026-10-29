// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Release plan 1.6 (#558; Fable on run 147, register OPS-18): a VM the
// platform does not delete answered «it is probably still being created …
// wait until it is running», though the VM was read in deleting. The
// refusal names the state it read and the way out of it (DeleteWayOut, one
// place with the deletes of the tools): deleting or an error — the console
// or support; still being created — wait and re-run. The reason stays
// API_ERROR and the record is kept.
func TestDestroy_RefusedDeleteNamesTheStateAndTheWayOut(t *testing.T) {
	for status, want := range map[string][]string{
		"deleting": {"deleting", "support"},
		"error":    {"error", "console"},
		"creating": {"creating", "again"},
	} {
		eng, cloud, _ := destroyFixture(t, runningRecord())
		cloud.deleteVMFn = func(context.Context, string) error {
			return clerrors.Wrap(clerrors.CodeAPIError, "API returned status 409", "",
				&provision.StatusError{Status: 409, Body: `{"code":"vm_can_not_be_deleted"}`})
		}
		cloud.getVMFn = func(_ context.Context, id string) (*provision.VM, error) {
			return &provision.VM{ID: id, Status: status}, nil
		}
		err := eng.Destroy(context.Background(), DestroyOptions{Force: true})
		var ce *clerrors.CLIError
		if !stderrors.As(err, &ce) || ce.Code != clerrors.CodeAPIError {
			t.Fatalf("%s: Destroy = %v, want API_ERROR", status, err)
		}
		text := ce.Message + " " + ce.Hint
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: %q does not say %q", status, text, w)
			}
		}
		if strings.Contains(text, "probably") {
			t.Errorf("%s: %q guesses the state it read", status, text)
		}
		if a := readActive(t, eng.StateDir); a == nil || a.Status != "running" {
			t.Errorf("%s: record after the refusal = %+v, want unchanged", status, a)
		}
	}
}
