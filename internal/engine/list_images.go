// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// ListImages fetches and displays available boot images from the Cloud.ru API.
func (e *Engine) ListImages(ctx context.Context) error {
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	e.Cloud.SetToken(tok.Value)

	images, err := e.Cloud.ListImages(ctx)
	if err != nil {
		return err
	}

	if len(images) == 0 {
		e.Printer.Success(i18n.T("No images found.", "Образы не найдены."))
		return nil
	}

	if e.Printer.IsJSON() {
		return e.Printer.JSON(images)
	}

	headers := []string{"ID", i18n.T("Name", "Имя"), i18n.T("OS", "ОС"), i18n.T("Status", "Состояние")}
	rows := make([][]string, 0, len(images))
	for _, img := range images {
		rows = append(rows, []string{
			img.ID,
			img.Name,
			img.OS,
			img.Status,
		})
	}

	e.Printer.Table(headers, rows)
	return nil
}
