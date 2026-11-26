// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

func newCleanupCmd() *cobra.Command {
	cmd := asGroup(&cobra.Command{
		Use: "cleanup",
		Short: i18n.T("Review and resolve pending floating IP receipts in this directory",
			"Просмотр и сверка незакрытых квитанций плавающих IP в этом каталоге"),
	})
	var deploymentID string
	list := &cobra.Command{
		Use: "receipts", Args: noArgs,
		Short: i18n.T("List the exact floating IP receipts that still need verification",
			"Показать точные квитанции плавающих IP, исход которых нужно проверить"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			eng, err := toolEngine(map[string]any{})
			if err != nil {
				return err
			}
			items, err := eng.PendingFloatingIPReceipts(deploymentID)
			if err != nil {
				return err
			}
			p := cmdPrinter(cmd)
			if p.IsJSON() {
				if items == nil {
					items = []engine.FloatingIPReceipt{}
				}
				return p.JSON(map[string]any{"receipts": items, "_untrusted": []string{"receipts"}})
			}
			if len(items) == 0 {
				p.Success(i18n.T("No pending floating IP receipts.", "Незакрытых квитанций плавающих IP нет."))
				return nil
			}
			for _, r := range items {
				rows := [][]string{{"receipt_id", r.ReceiptID}, {"deployment_id", r.DeploymentID}, {"kind", r.Kind}}
				for _, field := range [][]string{{"project_id", r.ProjectID}, {"vm_id", r.VMID}, {"vm_name", r.VMName}, {"name", r.Name}, {"attempt", r.Attempt}, {"address_id", r.AddressID}, {"address", r.Address}} {
					if field[1] != "" {
						rows = append(rows, field)
					}
				}
				if r.NeedsEvidenceProject {
					rows = append(rows, []string{"--evidence-project", i18n.T("Required: identify the project from the independent record.", "Нужен ID проекта из независимого свидетельства.")})
				}
				if !r.RequestedAt.IsZero() {
					rows = append(rows, []string{"requested_at", r.RequestedAt.UTC().Format(time.RFC3339Nano)})
				}
				p.Table([]string{i18n.T("Field", "Поле"), i18n.T("Receipt value", "Значение из квитанции")}, rows)
			}
			p.Hint(i18n.T("Check the original operation independently. Use 'evo cleanup resolve' only after the outcome of this exact request is established.",
				"Сверьте исход именно этого запроса по независимому свидетельству. Подтверждённый исход укажите в «evo cleanup resolve»."))
			return nil
		},
	}
	list.Flags().StringVar(&deploymentID, "deployment-id", "", i18n.T("Local deployment ID (optional for a single pending deployment)", "ID локального развёртывания (при одном незавершённом можно опустить)"))
	cmd.AddCommand(list, newCleanupResolveCmd())
	return cmd
}

func newCleanupResolveCmd() *cobra.Command {
	var req engine.FloatingIPReconciliation
	cmd := &cobra.Command{
		Use: "resolve <receipt-id>", Args: cobra.ExactArgs(1),
		Short: i18n.T("Record an independently verified outcome for one floating IP receipt",
			"Записать независимо проверенный исход одной квитанции плавающего IP"),
		Long: i18n.T("The owner checks the original operation for this exact receipt. Use not_created when it confirms that no address was allocated, or released with the independently identified address ID. For released, evo reads that ID and requires the service to confirm its absence. This action records the outcome locally. It only reads cloud resources. Run evo destroy again to complete the remaining cleanup.",
			"Владелец проверяет результат исходной операции для выбранной квитанции. Выберите not_created, если операция подтверждает, что адрес не выделялся. Для released укажите независимо установленный ID освобождённого адреса: evo проверит отсутствие адреса по ответу сервиса на чтение этого ID. Команда записывает результат на этой машине. К облачным ресурсам она обращается только для чтения. Затем повторите evo destroy для завершения уборки."),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.ReceiptID = args[0]
			if err := req.Validate(); err != nil {
				return err
			}
			eng, err := toolEngine(map[string]any{})
			if err != nil {
				return err
			}
			item, err := cleanupReceipt(eng, req)
			if err != nil {
				return err
			}
			if item.NeedsEvidenceProject && req.EvidenceProjectID == "" || req.EvidenceProjectID != "" && req.EvidenceProjectID != item.ProjectID {
				return clerrors.New(clerrors.CodeConfigInvalid, i18n.T("The project in the independent evidence must match the selected project.",
					"Проект из независимого свидетельства должен совпадать с выбранным проектом."), i18n.T("Check the original operation and supply its project with --evidence-project.", "Укажите проект исходной операции через --evidence-project."))
			}
			if item.AddressID != "" && item.AddressID != req.AddressID {
				return clerrors.New(clerrors.CodeUsage, i18n.T("The supplied outcome or address ID conflicts with the receipt.",
					"Указанный исход или ID адреса расходится с квитанцией."), i18n.T("Check the receipt and the independent evidence.", "Сверьте квитанцию и независимое свидетельство."))
			}
			summary := i18n.Tf("Record %s for receipt %q in deployment %q.", "Записать исход %s для квитанции %q развёртывания %q.", req.Outcome, output.SanitizeCell(item.ReceiptID), output.SanitizeCell(item.DeploymentID))
			details := map[string]any{"receipt": item, "outcome": req.Outcome, "evidence": req.Evidence, "_untrusted": []string{"receipt", "evidence", "address_id", "evidence_project"}}
			lines := []string{i18n.Tf("Evidence: %q", "Свидетельство: %q", output.SanitizeCell(req.Evidence))}
			if req.EvidenceProjectID != "" {
				details["evidence_project"] = req.EvidenceProjectID
				lines = append(lines, i18n.Tf("Independently identified project: %q", "Проект из независимого свидетельства: %q", output.SanitizeCell(req.EvidenceProjectID)))
			}
			if req.AddressID != "" {
				details["address_id"] = req.AddressID
				lines = append(lines, i18n.Tf("Read the released address by ID: %q", "Проверить освобождённый адрес по ID: %q", output.SanitizeCell(req.AddressID)))
			}
			ok, err := confirm.Ask(cmd.Context(), confirm.Preview{Action: "cleanup resolve", Summary: summary, Lines: lines, Details: details}, confirm.Options{
				Yes: flagYes, DryRun: flagDryRun, JSON: flagJSON, Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(),
			})
			if err != nil || !ok {
				return err
			}
			if err := eng.ReconcileFloatingIP(cmd.Context(), req); err != nil {
				return err
			}
			p := cmdPrinter(cmd)
			if p.IsJSON() {
				return p.JSON(map[string]any{"status": "reconciled", "deployment_id": req.DeploymentID, "receipt_id": req.ReceiptID, "outcome": req.Outcome, "_untrusted": []string{"deployment_id"}})
			}
			p.Success(i18n.Tf("Recorded %s for receipt %q.", "Записан исход %s для квитанции %q.", req.Outcome, output.SanitizeCell(req.ReceiptID)))
			p.Hint(i18n.T("Run 'evo destroy' again to finish cleanup.", "Повторите «evo destroy» для завершения уборки."))
			return nil
		},
	}
	cmd.Flags().StringVar(&req.DeploymentID, "deployment-id", "", i18n.T("Deployment ID shown by cleanup receipts", "ID развёртывания из cleanup receipts"))
	cmd.Flags().StringVar(&req.Outcome, "outcome", "", i18n.T("Verified outcome: not_created or released", "Подтверждённый исход: not_created или released"))
	cmd.Flags().StringVar(&req.AddressID, "address-id", "", i18n.T("Independently identified floating IP ID for released", "Независимо установленный ID плавающего IP для released"))
	cmd.Flags().StringVar(&req.EvidenceProjectID, "evidence-project", "", i18n.T("Project ID from independent evidence when the receipt has no recorded project", "ID проекта из независимого свидетельства, если в квитанции проект не записан"))
	cmd.Flags().StringVar(&req.Evidence, "evidence", "", i18n.T("Non-secret operation or audit reference proving this receipt's outcome", "Номер или ссылка на операцию либо аудит, подтверждающие исход этой квитанции, без секретов"))
	return cmd
}

func cleanupReceipt(eng *engine.Engine, req engine.FloatingIPReconciliation) (*engine.FloatingIPReceipt, error) {
	items, err := eng.PendingFloatingIPReceipts(req.DeploymentID)
	if err != nil {
		return nil, err
	}
	var selected *engine.FloatingIPReceipt
	for i := range items {
		if items[i].ReceiptID == req.ReceiptID {
			if selected != nil {
				return nil, clerrors.New(clerrors.CodeConfigInvalid, i18n.T("More than one pending receipt has this ID.", "Такой ID имеют несколько незакрытых квитанций."), "")
			}
			selected = &items[i]
		}
	}
	if selected == nil {
		return nil, clerrors.New(clerrors.CodeNotFound, i18n.T("No pending receipt matches this receipt ID and deployment.", "Незакрытой квитанции с таким ID у этого развёртывания нет."), i18n.T("List pending receipts with 'evo cleanup receipts'.", "Проверьте список командой «evo cleanup receipts»."))
	}
	return selected, nil
}
