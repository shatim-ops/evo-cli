// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// The same world as TestFault_Matrix_ListBehindALostCreate, for the cases
// whose cloud is not HTTP: the VM and its address (provision,
// deploy_apply), the rule of a security group, the backup and the disk of
// a restore (release plan 1.9, «Повтор платного создания после
// потерянного ответа», owner's decision 13.11, #1590, question 20). The
// answer of a create is lost and the lists show what it made lag reads
// later; the same call again must leave no more billable than one clean
// call. A read by id and the refusal of a taken name do not lag: the
// world changes one thing.
//
// That a service lists later than it takes the request is known for the
// disk of a restore (two runs of Г-16); for the others it is a hypothesis.

// listLagsComputePending are the tools of these worlds whose repeat
// leaves a second billable resource — each with what is known. Only
// shrinks. Closes by 1.10.
var listLagsComputePending = map[string]string{
	"provision": "the answer of the create of the address lost: the call itself reserves a second address, the first stays billed and bound to nothing; whether the list of addresses lags is not known (no run)",
}

func TestFault_Matrix_ListBehindALostCreate_Compute(t *testing.T) {
	const lag = 3
	type lagging interface{ lagBy(int) }
	writes := func(call string) bool {
		for _, verb := range []string{"Create", "Add", "Restore"} {
			if strings.HasPrefix(call, verb) {
				return true
			}
		}
		return false
	}
	found := map[string]string{}
	ran := map[string]int{}
	for _, c := range faultMatrixCases {
		happy, _, _, _ := faultRun(t, c, 0, "")
		if _, ok := happy.(lagging); !ok {
			continue // the HTTP world: TestFault_Matrix_ListBehindALostCreate
		}
		calls := happy.callLog()
		clean := len(happy.billed())
		for k := 1; k <= len(calls); k++ {
			if !writes(calls[k-1]) {
				continue
			}
			ctx, cancel := context.WithCancel(context.Background())
			w, r, _ := c.world(t, cancel)
			w.(lagging).lagBy(lag)
			w.failAt(k, faultLost)
			first, _ := r.handlers[c.tool](ctx, c.args)
			once := len(w.billed())
			w.disarm()
			again, _ := r.handlers[c.tool](context.Background(), c.args)
			cancel()
			ran[c.tool]++
			if n := len(w.billed()); n > clean && found[c.tool] == "" {
				found[c.tool] = fmt.Sprintf("the answer of %s lost and the lists %d reads behind: the call left %d billable and its repeat %d, one clean call leaves %d\nfirst: %.400s\nagain: %.400s",
					calls[k-1], lag, once, n, clean, first, again)
			}
		}
	}
	var tools []string
	for tool := range found {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	for _, tool := range tools {
		if listLagsComputePending[tool] == "" {
			t.Errorf("%s: %s", tool, found[tool])
		}
	}
	for tool := range listLagsComputePending {
		if found[tool] == "" {
			t.Errorf("listLagsComputePending names %s, whose repeat makes no second resource now: remove the line (the list only shrinks)", tool)
		}
	}
	for _, tool := range []string{"provision", "deploy_apply", "sg_rule_create", "backup_create", "backup_restore"} {
		if ran[tool] == 0 {
			t.Errorf("%s: no lost create was repeated behind a lagging list: the world no longer reaches it", tool)
		}
	}
	t.Logf("lost creates repeated behind lagging lists: %v; tools that made a second: %d (%s)", ran, len(tools), strings.Join(tools, ", "))
}
