// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// FB172-6 (delta audit of 1.7.2): where deploy decides the answer and the
// record of a step together (FB171-4), a write of the record that failed
// was dropped: the answer said the step's error, and the person did not
// learn that the state still said otherwise. The answer says the state was
// not updated.
func TestDeploy_DecidedRecordNotWrittenIsSaid(t *testing.T) {
	dep := newMockDeployer()
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	statePath := state.DefaultPath(dir)
	dep.hold = func(_ context.Context, op string) error {
		if op != "connect" {
			return nil
		}
		// The state cannot be written from here on: its file is now a
		// directory.
		if err := os.Remove(statePath); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(statePath, 0o700); err != nil {
			t.Error(err)
		}
		return clerrors.New(clerrors.CodeSSHAuthFailed, "ssh: unable to authenticate", "")
	}
	err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
	if clerrors.Reason(err) != clerrors.CodeSSHAuthFailed {
		t.Fatalf("err = %v, want SSH_AUTH_FAILED as before", err)
	}
	if !strings.Contains(err.Error()+" "+hintOf(err), "state was not updated") {
		t.Errorf("the record of the refused key was not written, and the answer does not say so: %v (hint %q)", err, hintOf(err))
	}
}
