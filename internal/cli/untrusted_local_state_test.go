// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"slices"
	"strings"
	"testing"
)

// The tools that answer from the state of the directory — its deployment,
// a saved plan, a planned stack — carry values the cloud gave beside evo's
// own: the VM's address, name, zone and state, the group and subnet a
// preview read, the IP and the floating IP an apply got, the outputs of a
// stack's steps. The invariant of the class (TestUntrustedClass_*,
// internal/tools) cannot reach them without that state; this world has it.
// Each field is in the answer and in its "_untrusted" (#2463).
func TestUntrusted_ToolsOfTheDirectoryMarkCloudValues(t *testing.T) {
	for _, c := range []struct {
		name, tool string
		record     bool
		args       map[string]any
		fields     []string
		// quoted are the values of the cloud evo's message names: in
		// quotes, on one line (quoteCloud).
		quoted []string
	}{
		{name: "status", tool: "status", record: true, fields: []string{"vm_ip", "vm_name", "zone_name", "cloud_state"}},
		{name: "deploy, a new VM", tool: "deploy", fields: []string{"vm", "security_group", "subnet_id"}},
		{name: "deploy, an update", tool: "deploy", record: true, fields: []string{"vm_ip", "vm_name", "upload"}, quoted: []string{`"evo-web"`}},
		{name: "deploy_apply", tool: "deploy_apply", args: map[string]any{"confirmed": true}, fields: []string{"public_ip", "floating_ip_id", "ssh", "note"}},
		{name: "stack_plan of a VM", tool: "stack_plan", args: map[string]any{"manifest": map[string]any{"name": "s",
			"vm": map[string]any{"image": "Ubuntu 22.04", "zone": pvZone}}}, fields: []string{"vm"}},
		{name: "stack_apply", tool: "stack_apply", args: map[string]any{"confirmed": true}, fields: []string{"outputs"}},
		{name: "stack_status", tool: "stack_status", fields: []string{"outputs"}},
		{name: "stack_destroy", tool: "stack_destroy", fields: []string{"resources"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			srv := parityServers(t)
			args := map[string]any{}
			for k, v := range parityArgs[c.tool] {
				args[k] = v
			}
			setup := paritySetup[c.tool]
			if c.record {
				setup = recordDeployment
			}
			if setup != nil {
				more, undo := setup(t, w)
				t.Cleanup(undo)
				for k, v := range more {
					args[k] = v
				}
			}
			for k, v := range c.args {
				args[k] = v
			}
			m := stdioDetails(t, srv.stdio, c.tool, args)
			marked, _ := m["_untrusted"].([]any)
			for _, f := range c.fields {
				if _, ok := m[f]; !ok {
					t.Errorf("no %s in the answer: the world did not reach it: %v", f, m)
					continue
				}
				if !slices.Contains(marked, any(f)) {
					t.Errorf("%s carries a value of the cloud outside _untrusted %v", f, marked)
				}
			}
			msg, _ := m["message"].(string)
			for _, q := range c.quoted {
				if !strings.Contains(msg, q) {
					t.Errorf("the message names the cloud's %s unquoted: %s", q, msg)
				}
			}
		})
	}
}
