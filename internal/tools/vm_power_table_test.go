// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// vmStateRow is what a power tool answers for a VM in a state — a row of
// the table «state → answer» of its section of the brief
// (docs/internal/briefs/ops-wave-1.md, §3.1).
type vmStateRow string

const (
	// The preview and the question; the command once, after the yes.
	rowAsks vmStateRow = "asks"
	// As rowAsks, and the preview warns of the state.
	rowAsksWarned vmStateRow = "asks with a warning"
	// The change is made: the action's already, no question, no command.
	rowAlready vmStateRow = "already"
	// The action's own transition: waited for, no question, no command.
	rowWaits vmStateRow = "waits"
	// USAGE that names the state, no question, no command.
	rowRefused vmStateRow = "refused"
)

// vmStateUnknown stands for a state evo does not know: the platform
// decides, the preview names it.
const vmStateUnknown = "paused"

// vmPowerStateRows is the table «state → answer» of every power tool, a
// world per row (the brief, «Тесты до кода: мир на каждую строку
// таблицы»). The product's own tables decide which states a tool must
// have a row for; the rows say what is asked and what is written. A
// closed list: a power tool added to vmPowerActions with no rows here, and
// a state added to the product's tables with no row, fail the test.
var vmPowerStateRows = map[string]map[string]vmStateRow{
	"vm_start": {
		"stopped":        rowAsks,
		"running":        rowAlready,
		"starting":       rowWaits,
		"stopping":       rowRefused,
		"rebooting":      rowRefused,
		"creating":       rowRefused,
		"updating":       rowRefused,
		"deleting":       rowRefused,
		"error_deleting": rowRefused,
		"error_creating": rowRefused,
		"error":          rowAsksWarned,
		vmStateUnknown:   rowAsksWarned,
	},
	// The brief, §4.1.
	"vm_stop": {
		"running":        rowAsks,
		"stopped":        rowAlready,
		"stopping":       rowWaits,
		"starting":       rowRefused,
		"rebooting":      rowRefused,
		"creating":       rowRefused,
		"updating":       rowRefused,
		"deleting":       rowRefused,
		"error_deleting": rowRefused,
		"error_creating": rowRefused,
		"error":          rowAsksWarned,
		vmStateUnknown:   rowAsksWarned,
	},
}

// vmStatesOfTheProduct are the states the planner of a power tool decides
// by: the action's own, the ones it waits out, the ones there is nothing
// to change in, error, and one it does not know.
func vmStatesOfTheProduct(act vmPowerAction) []string {
	set := map[string]bool{act.from: true, act.via: true, act.to: true, "error": true, vmStateUnknown: true}
	for st := range vmSettling {
		set[st] = true
	}
	for st := range vmNothingToChange {
		set[st] = true
	}
	var out []string
	for st := range set {
		out = append(out, st)
	}
	sort.Strings(out)
	return out
}

func TestVMPower_EveryStateOfTheTableHasAWorld(t *testing.T) {
	for tool := range vmPowerStateRows {
		if _, ok := vmPowerActions[tool]; !ok {
			t.Errorf("vmPowerStateRows names %s, which is not a power tool (vmPowerActions): remove its rows", tool)
		}
	}
	for tool, act := range vmPowerActions {
		rows := vmPowerStateRows[tool]
		if rows == nil {
			t.Errorf("%s is a power tool (vmPowerActions) with no rows in vmPowerStateRows: write the table «state → answer» of its section of the brief, a row per state", tool)
			continue
		}
		known := map[string]bool{}
		for _, state := range vmStatesOfTheProduct(act) {
			known[state] = true
			if rows[state] == "" {
				t.Errorf("%s: the planner decides by the state %s, and vmPowerStateRows has no row for it: say what is asked and what is written", tool, state)
			}
		}
		for state, row := range rows {
			if !known[state] {
				t.Errorf("%s: the row of %s names a state the product's tables no longer do (vmPowerActions, vmSettling, vmNothingToChange): the state is decided as an unknown one — put it back, or remove the row", tool, state)
				continue
			}
			for _, confirmed := range []bool{false, true} {
				r, w := vmStartWorld(t, state)
				m, text, isErr := callNamed(t, r, tool, map[string]any{"name": "web", "confirmed": confirmed})
				warning, _ := m["warning"].(string)
				asked := m["status"] == "confirmation_required"
				wantCalls := 0
				var problem string
				switch row {
				case rowAsks, rowAsksWarned:
					if confirmed {
						wantCalls = 1
					} else if !asked {
						problem = "no question"
					} else if (row == rowAsksWarned) != strings.Contains(warning, state) {
						problem = fmt.Sprintf("the warning of the preview: %q", warning)
					}
				case rowAlready:
					if isErr || m["status"] != act.already {
						problem = "not " + act.already
					}
				case rowWaits:
					if isErr || asked {
						problem = "asked or refused"
					}
				case rowRefused:
					if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, state) {
						problem = "not a USAGE that names the state"
					}
				}
				if problem != "" {
					t.Errorf("%s of a VM in %s (%s), confirmed=%v: %s: %s", tool, state, row, confirmed, problem, text)
				}
				if n := w.powerCalls(); n != wantCalls {
					t.Errorf("%s of a VM in %s (%s), confirmed=%v: %d power commands, want %d: %s", tool, state, row, confirmed, n, wantCalls, text)
				}
			}
		}
	}
}
