// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors_test

import (
	stderrors "errors"
	"fmt"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func TestNew(t *testing.T) {
	err := clierrors.New(clierrors.CodeAuthRequired, "authentication required", "run 'vm auth login'")

	if err.Code != clierrors.CodeAuthRequired {
		t.Errorf("Code = %q, want %q", err.Code, clierrors.CodeAuthRequired)
	}
	if err.Message != "authentication required" {
		t.Errorf("Message = %q, want %q", err.Message, "authentication required")
	}
	if err.Hint != "run 'vm auth login'" {
		t.Errorf("Hint = %q, want %q", err.Hint, "run 'vm auth login'")
	}
	if err.Err != nil {
		t.Errorf("Err = %v, want nil", err.Err)
	}
}

func TestError(t *testing.T) {
	err := clierrors.New(clierrors.CodeVMNotFound, "vm not found", "")
	got := err.Error()
	want := "VM_NOT_FOUND: vm not found"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestUnwrap(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := clierrors.Wrap(clierrors.CodeAPIUnreachable, "cannot reach API", "check network", cause)

	unwrapped := err.Unwrap()
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}
}

func TestIs(t *testing.T) {
	err1 := clierrors.New(clierrors.CodeSSHTimeout, "timeout connecting", "check firewall")
	err2 := clierrors.New(clierrors.CodeSSHTimeout, "different message", "different hint")

	if !stderrors.Is(err1, err2) {
		t.Error("errors.Is should return true for CLIErrors with the same Code")
	}
}

func TestIs_DifferentCode(t *testing.T) {
	err1 := clierrors.New(clierrors.CodeSSHTimeout, "timeout", "")
	err2 := clierrors.New(clierrors.CodeSSHAuthFailed, "auth failed", "")

	if stderrors.Is(err1, err2) {
		t.Error("errors.Is should return false for CLIErrors with different Codes")
	}
}

func TestWrap(t *testing.T) {
	cause := fmt.Errorf("original failure")
	err := clierrors.Wrap(clierrors.CodeComposeInvalid, "invalid compose file", "check syntax", cause)

	if !stderrors.Is(err, &clierrors.CLIError{Code: clierrors.CodeComposeInvalid}) {
		t.Error("wrapped error should match by Code via errors.Is")
	}

	var target *clierrors.CLIError
	if !stderrors.As(err, &target) {
		t.Fatal("errors.As should succeed for *CLIError")
	}
	if target.Code != clierrors.CodeComposeInvalid {
		t.Errorf("Code = %q, want %q", target.Code, clierrors.CodeComposeInvalid)
	}

	if !stderrors.Is(err.Unwrap(), cause) {
		t.Error("Unwrap should return the original cause")
	}
}
