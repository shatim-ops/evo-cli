// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// QA-14 (QA 20.10): logs --tail -5 показывал весь журнал, как 0. Теперь
// USAGE до SSH: ни одной команды на ВМ.
func TestLogs_NegativeTailIsUsage(t *testing.T) {
	eng, dep, _ := engineWithDeployment(t, verifyRecord)
	err := eng.Logs(context.Background(), LogsOptions{Tail: -5})
	var ce *clerrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clerrors.CodeUsage || clerrors.ExitCode(err) != clerrors.ExitGeneral {
		t.Fatalf("err = %v, want USAGE", err)
	}
	if len(dep.execCalls) != 0 {
		t.Errorf("ran on the VM: %q", dep.execCalls)
	}
}
