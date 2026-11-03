// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"
)

// P6 (agent runs of Grok on 1.6.0; release plan 1.7, «Метка правила
// порта»): a port rule opened for a VM from folder A is closed when the VM
// is deleted from folder B — its state knows nothing of the rule, the rule's
// label (evo: port N (vm <id>)) does. Only the rules of the VM the call
// deletes go: a rule of another VM that lives stays, whichever folder
// opened it.
func TestPortRule_OpenedInFolderAClosedByDeleteFromFolderB(t *testing.T) {
	cloud := &faultCloud{}
	a, _ := faultToolsRegistry(t, cloud)
	b := budgetComputeRegistry(t, cloud, t.TempDir(), faultToolsTimings())

	for _, vm := range []struct{ name, port string }{{"web", "8080"}, {"api", "9090"}} {
		if text, isErr := a.handlers["provision"](context.Background(), map[string]any{"name": vm.name, "ports": []any{vm.port}, "confirmed": true}); isErr {
			t.Fatalf("provision %s from folder A: %s", vm.name, text)
		}
	}
	var webID string
	for _, vm := range cloud.liveVMs() {
		if vm.Name == "web" {
			webID = vm.ID
		}
	}
	if webID == "" || !portRuleLive(cloud, "8080") || !portRuleLive(cloud, "9090") {
		t.Fatalf("the world before the delete: VM web %q, rules %+v", webID, cloud.liveRules())
	}

	text, isErr := b.handlers["destroy"](context.Background(), map[string]any{"vm_id": webID, "confirmed": true})
	if isErr {
		t.Fatalf("destroy vm_id=%s from folder B: %s", webID, text)
	}
	for _, vm := range cloud.liveVMs() {
		if vm.ID == webID {
			t.Fatalf("VM %s is still there after destroy from folder B: %s", webID, text)
		}
	}
	if portRuleLive(cloud, "8080") {
		t.Errorf("the rule of port 8080, opened for VM %s from folder A, is left after the VM was deleted from folder B (P6): %+v\n%s", webID, cloud.liveRules(), text)
	}
	if !portRuleLive(cloud, "9090") {
		t.Errorf("the rule of port 9090 of VM api, which lives, was closed by the delete of VM %s: %+v", webID, cloud.liveRules())
	}
}

// portRuleLive reports whether an ingress rule of port is in the world.
func portRuleLive(c *faultCloud, port string) bool {
	for _, r := range c.liveRules() {
		if r.Direction == "ingress" && (r.PortRange == port+":"+port || r.PortRange == port || strings.HasPrefix(r.PortRange, port+"-"+port)) {
			return true
		}
	}
	return false
}
