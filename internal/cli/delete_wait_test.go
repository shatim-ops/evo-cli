// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"maps"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// D74 (the owner's decision 03.11): each of the ten deletes that read their
// outcome, and dns_zone_delete (D66) on the same point, takes --wait=false, from the schema's argument wait, and answers
// at once as 'evo tools call' with wait=false does: deleting with the
// operation_id, exit 0.
func TestDeleteWaitFalse_TreeEqualsToolsCall(t *testing.T) {
	w := withParityWorld(t)
	reg := tools.New(nil)
	deletes := map[string]bool{"ar_delete": true, "ca_delete": true, "dns_record_delete": true, "k8s_delete": true, "k8s_nodepool_delete": true,
		"kafka_delete": true, "lb_delete": true, "tg_delete": true, "pg_delete": true, "redis_delete": true, "vpc_delete": true, "dns_zone_delete": true}
	seen := 0
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			if !deletes[c.Tool] {
				continue
			}
			seen++
			tl, _ := reg.Lookup(c.Tool)
			args := maps.Clone(parityArgs[c.Tool])
			args["wait"] = false
			pc := parityCommandOf(t, g, c, tl, args)
			extra := []string{"--json", "--yes"}
			newProcess()
			treeCode, treeOut, treeErr := runCLI(t, "", append(append([]string{}, pc.tree...), extra...)...)
			newProcess()
			w.rt.newCall()
			callCode, callOut, _ := runCLI(t, "", append(append([]string{}, pc.call...), extra...)...)
			w.rt.newCall()
			if treeCode != 0 || treeCode != callCode || sameVolatile(treeOut) != sameVolatile(callOut) {
				t.Errorf("evo %s ≠ evo tools call %s: exit %d/%d\n  tree: %s %s\n  call: %s",
					strings.Join(pc.tree, " "), c.Tool, treeCode, callCode, clipLine(treeOut), clipLine(treeErr), clipLine(callOut))
				continue
			}
			if obj := parseObject(treeOut); obj["status"] != "deleting" || obj["operation_id"] == nil {
				t.Errorf("evo %s: want deleting with operation_id at once, got %s", strings.Join(pc.tree, " "), clipLine(treeOut))
			}
			// FB170-4: --wait=false of evo tools call is the tool's wait, as
			// --wait=false of the tree, not a flag the call drops.
			var flag []string
			for i := 0; i < len(pc.call); i++ {
				if pc.call[i] == "--arg" && pc.call[i+1] == "wait=false" {
					i++
					continue
				}
				flag = append(flag, pc.call[i])
			}
			newProcess()
			flagCode, flagOut, flagErr := runCLI(t, "", append(append(flag, "--wait=false"), extra...)...)
			w.rt.newCall()
			if flagCode != treeCode || sameVolatile(flagOut) != sameVolatile(treeOut) {
				t.Errorf("evo tools call %s --wait=false ≠ evo %s: exit %d/%d\n  call: %s %s",
					c.Tool, strings.Join(pc.tree, " "), flagCode, treeCode, clipLine(flagOut), clipLine(flagErr))
			}
		}
	}
	if seen != len(deletes) {
		t.Errorf("commands of the deletes: %d of %d", seen, len(deletes))
	}
}
