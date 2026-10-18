// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"fmt"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/verify"
)

// Verify probes the exposed ports of the active deployment and prints the
// results. An unhealthy service is an error (exit 1): scripts and CI must be
// able to tell a broken deployment from a working one.
func (e *Engine) Verify(ctx context.Context) error {
	stateFile, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		return err
	}

	// Nothing deployed is not "healthy": verify, like logs, inspects a
	// running deployment and exits 4 without one (DECISIONS D11), so a
	// script's "deploy && verify" cannot pass on an empty directory.
	active := stateFile.ActiveDeployment()
	if active == nil {
		return clerrors.New(clerrors.CodeVMNotFound,
			i18n.T("no active deployment to verify", "активного развёртывания нет, проверять нечего"),
			deployFirst())
	}
	if len(active.ExposedPorts) == 0 {
		if e.Printer.IsJSON() {
			return e.Printer.JSON([]verify.ProbeResult{})
		}
		e.Printer.Success(i18n.T("No exposed ports to verify.", "Открытых портов нет, проверять нечего."))
		return nil
	}

	probe := e.Probe
	if probe == nil {
		probe = verify.Check
	}
	results := probe(ctx, active.VMIP, active.ExposedPorts, e.timings().VerifyTimeout)

	unhealthy := 0
	for _, r := range results {
		if !r.Healthy {
			unhealthy++
		}
	}

	if e.Printer.IsJSON() {
		if err := e.Printer.JSON(results); err != nil {
			return err
		}
	} else {
		headers := []string{i18n.T("Port", "Порт"), i18n.T("Status", "Ответ"), i18n.T("Healthy", "Работает"), i18n.T("Latency", "Задержка")}
		var rows [][]string
		for _, r := range results {
			healthy := i18n.T("yes", "да")
			status := fmt.Sprintf("%d", r.StatusCode)
			if r.Protocol == "tcp" {
				status = i18n.T("tcp open", "tcp открыт")
			}
			if !r.Healthy {
				healthy = i18n.T("no", "нет")
				if r.Error != "" {
					status = r.Error
				}
			}
			rows = append(rows, []string{
				fmt.Sprintf("%d", r.Port),
				status,
				healthy,
				r.Latency.Round(time.Millisecond).String(),
			})
		}
		e.Printer.Table(headers, rows)
	}

	if unhealthy > 0 {
		return clerrors.New(clerrors.CodeUnhealthy,
			i18n.Tf("%d of %d health check(s) failed on %s", "не прошли проверки: %d из %d на %s", unhealthy, len(results), active.VMIP),
			i18n.T("Check the service logs with 'evo logs'.", "Посмотрите журналы сервисов: 'evo logs'."))
	}
	return nil
}
