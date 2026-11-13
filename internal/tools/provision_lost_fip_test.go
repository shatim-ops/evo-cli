// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// The create of provision's floating IP got no answer and the list of
// addresses is behind (#1617): the call stops before the VM, naming the
// address; its repeat over MCP sends nothing and asks, with the address
// and its price in the question and the read that shows it; with the
// person's yes the next call reserves one — and the first address's mark
// stays (D107; «Риск», conclusion 180, У1, У2, У5, Т1, Т2, Т4).
func TestProvision_LostFloatingIP_StopsThenAsks(t *testing.T) {
	var c faultMatrixCase
	for _, m := range faultMatrixCases {
		if m.tool == "provision" {
			c = m
		}
	}
	type lagging interface{ lagBy(int) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, r, dir := c.world(t, cancel)
	w.(lagging).lagBy(100)
	fipCreates := func() int {
		n := 0
		for _, call := range w.callLog() {
			if strings.HasPrefix(call, "CreateFloatingIP") {
				n++
			}
		}
		return n
	}
	vmCreates := func() int {
		n := 0
		for _, call := range w.callLog() {
			if strings.HasPrefix(call, "CreateVM") {
				n++
			}
		}
		return n
	}
	// The create of the boot address, its answer lost.
	w.failAt(firstWrite(t, c, "CreateFloatingIP"), faultLost)
	first, _ := r.handlers["provision"](ctx, c.args)
	w.disarm()
	var a map[string]any
	_ = json.Unmarshal([]byte(first), &a)
	name, _ := a["floating_ip_name"].(string)
	if a["status"] != "error" || name == "" || fipCreates() != 1 || vmCreates() != 0 {
		t.Fatalf("first call: FIP creates %d, VM creates %d, answer %s; want a stop before the VM naming the address", fipCreates(), vmCreates(), first)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil || len(sf.LostFloatingIPs) != 1 || sf.LostFloatingIPs[0].Name != name {
		t.Fatalf("marks %+v (%v), want the mark of %s", sf.LostFloatingIPs, err, name)
	}

	again, _ := r.handlers["provision"](context.Background(), c.args)
	var q map[string]any
	_ = json.Unmarshal([]byte(again), &q)
	in, _ := q["interrupted"].(map[string]any)
	if q["status"] != "confirmation_required" || in["floating_ip"] != name || in["read_tool"] != "list_floating_ips" || in["floating_ip_monthly_rub"] == nil {
		t.Fatalf("repeat: %s; want the question naming %s, the read and the price", again, name)
	}
	if msg, _ := q["message"].(string); !strings.Contains(msg, "person") || !strings.Contains(msg, "No second address was requested") {
		t.Fatalf("question: %s", msg)
	}
	if fipCreates() != 1 || vmCreates() != 0 {
		t.Fatalf("the repeat sent: FIP creates %d, VM creates %d", fipCreates(), vmCreates())
	}

	// The question went to the model: with the person's yes the call goes.
	done, _ := r.handlers["provision"](context.Background(), c.args)
	var d map[string]any
	_ = json.Unmarshal([]byte(done), &d)
	if d["status"] != "ok" || fipCreates() != 2 || vmCreates() != 1 {
		t.Fatalf("after the yes: FIP creates %d, VM creates %d, answer %s", fipCreates(), vmCreates(), done)
	}
	if sf, _ := state.Read(state.DefaultPath(dir)); len(sf.LostFloatingIPs) != 1 || !sf.LostFloatingIPs[0].Answered {
		t.Fatalf("marks %+v; want the first address's mark kept, answered", sf.LostFloatingIPs)
	}
}

// firstWrite is the number of the first call named call in a clean run of
// c: the k a world fails at.
func firstWrite(t *testing.T, c faultMatrixCase, call string) int {
	t.Helper()
	happy, _, _, _ := faultRun(t, c, 0, "")
	for k, got := range happy.callLog() {
		if strings.HasPrefix(got, call) {
			return k + 1
		}
	}
	t.Fatalf("a clean %s makes no %s", c.tool, call)
	return 0
}
