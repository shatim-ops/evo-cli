// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// interfaceRow retains null for an unassigned VM and [] for an explicitly
// empty group list. Both fields also serve existing readers that omit them.
type interfaceRow struct {
	*provision.Interface
	VM             **provision.InterfaceVM     `json:"vm,omitempty"`
	SecurityGroups *[]provision.InterfaceGroup `json:"security_groups,omitempty"`
}

func networkInterfaceRow(nic *provision.Interface) interfaceRow {
	row := interfaceRow{Interface: nic}
	if nic.VMReported || nic.VM != nil {
		row.VM = &nic.VM
	}
	if nic.SecurityGroups != nil {
		row.SecurityGroups = &nic.SecurityGroups
	}
	return row
}

// projectInterfaces is also the source for the by-ID ownership check.
// Reject a contradictory answer before returning any rows or proving ownership.
func projectInterfaces(ctx context.Context, eng *engine.Engine) ([]*provision.Interface, error) {
	project := eng.Config.ProjectID
	rows, err := eng.Cloud.ListInterfaces(ctx, project)
	if err != nil {
		return nil, err
	}
	for _, nic := range rows {
		if nic == nil || nic.ID == "" || (nic.ProjectID != "" && nic.ProjectID != project) {
			return nil, clierrors.New(clierrors.CodeAPIError,
				i18n.T("Compute returned an invalid interface in the project list", "Compute вернул некорректный интерфейс в списке проекта"), "")
		}
	}
	return rows, nil
}

func (s *Registry) handleInterfaceList(ctx context.Context, _ map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	items, err := projectInterfaces(ctx, eng)
	if err != nil {
		return errResult(err), true
	}
	rows := make([]interfaceRow, 0, len(items))
	for _, nic := range items {
		rows = append(rows, networkInterfaceRow(nic))
	}
	return toJSON(map[string]any{"interfaces": rows}), false
}

var kindInterface = withCard(inProjectList("network interface", "interface_list", computeList(func(ctx context.Context, eng *engine.Engine) ([]string, error) {
	items, err := projectInterfaces(ctx, eng)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(items))
	for i, nic := range items {
		ids[i] = nic.ID
	}
	return ids, nil
})), func(ctx context.Context, c *ownerCall, id string) (map[string]any, error) {
	eng, err := c.engine(ctx)
	if err != nil {
		return nil, err
	}
	nic, err := eng.Cloud.GetInterface(ctx, id)
	if err != nil {
		return nil, err
	}
	// Doctor and history use the actual resource card to establish existence.
	return map[string]any{"id": nic.ID, "name": nic.Name, "state": nic.State}, nil
})

func (s *Registry) handleInterfaceGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "interface_id")
	if !isUUID(id) {
		return usageT("'interface_id' must be a UUID from interface_list", "нужен UUID интерфейса из списка интерфейсов"), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	card, err := eng.Cloud.GetInterface(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	if s.projectBound && card.ProjectID != "" && card.ProjectID != eng.Config.ProjectID {
		return errResult(notInProject(kindInterface, id)), true
	}
	return toJSON(map[string]any{"interface": networkInterfaceRow(card)}), false
}
