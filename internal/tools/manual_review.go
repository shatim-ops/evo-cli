// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"errors"
	"os"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// CheckManualIntentScope uses the project and repeat rules of the exact tool.
// In particular, a manual review cannot change the evidence for a network retry.
func CheckManualIntentScope(in intent.Intent, cfg config.Config) error {
	if everySendIsAChange[in.Tool].listedName != nil {
		return errors.New(i18n.T("This receipt protects a network create from being repeated. Verify its target with the original key.", "Эта квитанция защищает от повторного создания сетевого ресурса. Проверьте цель исходным ключом."))
	}
	eng := &engine.Engine{Config: cfg}
	if !decls[in.Tool].Project || projectOfCall(eng, in, nil) != cfg.ProjectID || in.Name == "" && in.Target == "" {
		return errors.New(i18n.T("Cannot establish the project's scope and exact target of this operation.", "Не удалось установить область проекта и точную цель этой операции."))
	}
	return nil
}

// manualReviewClosed affects diagnostic output only. A failed read always
// leaves the receipt visible. Cloud operations and history use the original.
func manualReviewClosed(root string, in intent.Intent, eng *engine.Engine) bool {
	if eng == nil || !intent.HasManualDecisions() || CheckManualIntentScope(in, eng.Config) != nil {
		return false
	}
	snap, err := intent.PreviewManual(root, in.ID, eng.Config.Profile)
	if err != nil {
		return false
	}
	defer snap.Close()
	if !sameReceiptDirectory(in.Dir, snap.Record.Dir) || snap.Eligible(eng.Maker(), eng.Config.ProjectID) != nil {
		return false
	}
	closed, err := snap.Marked()
	return err == nil && closed
}

func sameReceiptDirectory(a, b string) bool {
	x, err := os.Stat(a)
	if err != nil {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && os.SameFile(x, y)
}
