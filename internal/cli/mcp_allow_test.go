// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	stderrors "errors"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The refusal of a project outside --allow-project is marked, so that the
// owner check of a resource answers it as NOT_FOUND and any other error of
// the factory as it is (FB121-5).
func TestCheckAllowedProject_MarksTheRefusal(t *testing.T) {
	if err := checkAllowedProject([]string{"p-1"}, "p-2"); !stderrors.Is(err, tools.ErrProjectNotAllowed) {
		t.Errorf("refusal not marked: %v", err)
	}
	if err := checkAllowedProject([]string{"p-1"}, "p-1"); err != nil {
		t.Errorf("allowed project refused: %v", err)
	}
}
