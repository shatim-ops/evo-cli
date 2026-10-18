// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

func TestReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"plain error", fmt.Errorf("boom"), clierrors.ReasonGeneral},
		{"cli error", clierrors.New(clierrors.CodeAuthFailed, "m", "h"), "AUTH_FAILED"},
		{"wrapped", fmt.Errorf("ctx: %w", clierrors.New(clierrors.CodeVMNotFound, "m", "")), "VM_NOT_FOUND"},
		{"interrupted", context.Canceled, clierrors.ReasonInterrupted},
		{"interrupted cli error", fmt.Errorf("%w: %w", context.Canceled,
			clierrors.New(clierrors.CodeVMProvisionFailed, "m", "")), clierrors.ReasonInterrupted},
	}
	for _, tt := range tests {
		if got := clierrors.Reason(tt.err); got != tt.want {
			t.Errorf("%s: Reason = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestExitCodesHelp_Language(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang("ru")
	if h := clierrors.ExitCodesHelp(); !strings.HasPrefix(h, "Коды выхода:") || !strings.Contains(h, "130  прервано") {
		t.Errorf("ru:\n%s", h)
	}
	i18n.SetLang("en")
	if h := clierrors.ExitCodesHelp(); !strings.HasPrefix(h, "Exit codes:") {
		t.Errorf("en:\n%s", h)
	}
}
