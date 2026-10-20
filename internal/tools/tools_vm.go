// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// handleVMGet returns LIVE details for a single VM (by vm_id or name): its
// private IP, any public/floating IP, state, and every network interface.
//
// This closes a real gap: the `list` and `status` tools read local state and do
// NOT expose a VM's private IP, so an agent that needed a freshly-created VM's
// private address (e.g. to wire a service endpoint) had no tool for it and would
// fall back to raw shell/API probing. GetVMDetail already carries the data
// (interfaces[].ip_address); this tool surfaces and normalizes it.
//
// IP normalization is grounded in the real API shape:
//   - a "regular" interface's ip_address is the PRIVATE (subnet) address;
//   - a "direct_ip" interface's ip_address is a directly-attached PUBLIC address;
//   - a floating_ip (when present on a regular interface) is its PUBLIC address.
//
// Read-only; touches eng.Cloud, so it uses authedEngine.
// vmRefNotBoth is the refusal of a call with both vm_id and name (vm_get,
// destroy).
func vmRefNotBoth() error {
	return clierrors.New(clierrors.CodeUsage,
		i18n.T("pass vm_id or name, not both", "укажите vm_id или name, не оба"),
		i18n.T("vm_id is the VM's UUID; name finds the VM by its name.",
			"vm_id — UUID ВМ; по name ВМ ищется по имени."))
}

func (s *Registry) handleVMGet(ctx context.Context, args map[string]any) (string, bool) {
	// Both: the vm_id was taken and the name dropped without a word (QA
	// 20.10, QA-11), as release_floating_ip did with id and ip_address; one
	// of only spaces was dropped as well (break r4, R4-11).
	ref, _, err := oneOfArgs(args, vmRefNotBoth(), "vm_id", "name")
	if err != nil {
		return errResult(err), true
	}
	if ref == "" {
		return usageT("vm_id or name is required", "нужен vm_id или name"), true
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}

	// Accept either a UUID or a human name.
	vmID := ref
	if !looksUUID(ref) {
		id, _, e := s.resolveVM(ctx, ref, eng.Config.ProjectID)
		if e != nil {
			return errResult(e), true
		}
		vmID = id
	}

	det, err := eng.Cloud.GetVMDetail(ctx, vmID)
	if err != nil {
		return errResult(err), true
	}

	var privateIP, publicIP string
	ifaces := make([]map[string]any, 0, len(det.Interfaces))
	for _, in := range det.Interfaces {
		row := map[string]any{
			"id":         in.ID,
			"name":       in.Name,
			"ip_address": in.IPAddress,
			"type":       in.Type,
			"primary":    in.Primary,
			"state":      in.State,
		}
		if in.FloatingIP != nil && in.FloatingIP.IPAddress != "" {
			row["floating_ip"] = in.FloatingIP.IPAddress
			if publicIP == "" || in.Primary {
				publicIP = in.FloatingIP.IPAddress
			}
		}
		if in.Type == "direct_ip" {
			// The interface IP itself is the public address.
			if in.IPAddress != "" && (publicIP == "" || in.Primary) {
				publicIP = in.IPAddress
			}
		} else if in.IPAddress != "" && (privateIP == "" || in.Primary) {
			// "regular" (or unspecified) => subnet-internal private IP.
			privateIP = in.IPAddress
		}
		ifaces = append(ifaces, row)
	}

	return toJSON(map[string]any{
		"id":         det.ID,
		"name":       det.Name,
		"state":      det.State,
		"private_ip": privateIP,
		"public_ip":  publicIP,
		"interfaces": ifaces,
	}), false
}

// handleVMList lists the project's VMs from the live API (read-only).
func (s *Registry) handleVMList(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	vms, err := eng.Cloud.ListVMsInProject(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(vms))
	for _, v := range vms {
		row := map[string]any{
			"id":    v.ID,
			"name":  v.Name,
			"state": v.Status,
			"zone":  v.ZoneID(),
		}
		if v.AvailabilityZone != nil && v.AvailabilityZone.Name != "" {
			row["zone"] = v.AvailabilityZone.Name
		}
		if ip := v.PublicIP; ip != "" {
			row["public_ip"] = ip
		} else if v.FloatingIP != "" {
			row["public_ip"] = v.FloatingIP
		}
		if !v.CreatedAt.IsZero() {
			row["created_time"] = v.CreatedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"vms": out}), false
}
