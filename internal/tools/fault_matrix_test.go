// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// faultMatrixCase is a billable tool whose cloud the fault world models:
// args make it create, setup readies its directory (a saved plan).
type faultMatrixCase struct {
	tool  string
	args  map[string]any
	setup func(t *testing.T, dir string)
	// noDial: the tool dials TCP/22 of the VM it made (deploy_apply).
	noDial bool
}

var faultMatrixCases = []faultMatrixCase{
	{tool: "provision", args: map[string]any{"name": "web", "confirmed": true}},
	{tool: "deploy_apply", args: map[string]any{"plan_id": "plan-0123456789ab", "confirmed": true}, noDial: true,
		setup: func(t *testing.T, dir string) {
			if err := deployplan.Save(dir, faultPlan()); err != nil {
				t.Fatal(err)
			}
		}},
}

// The fault kinds a call can meet at any step. faultLostNameless is not one:
// it is an answer the API cannot give (a floating IP's name is required,
// minLength 1), weighed and left in BACKLOG.md (break-r3a).
var faultMatrixKinds = []string{faultCancel, faultTransport, faultLost, faultEmpty, faultAuth, faultReject, faultHTTP500}

// fipPollWait is the production poll of a floating IP (engine
// fipPollInterval): a fault run that waits one has missed the world's
// Timings.
const fipPollWait = 5 * time.Second

// faultRun is one call of a case on a fresh world, failing at call k with
// kind (k == 0: no failure). It returns the world, the answer and the
// state directory.
func faultRun(t *testing.T, c faultMatrixCase, k int, kind string) (*faultCloud, string, string, *Registry) {
	t.Helper()
	cloud := &faultCloud{noDial: c.noDial}
	r, dir := faultToolsRegistry(t, cloud)
	if c.setup != nil {
		c.setup(t, dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cloud.cancel = cancel
	if k > 0 {
		cloud.failAt(k, kind)
	}
	start := time.Now()
	text, _ := r.handlers[c.tool](ctx, c.args)
	// The world's Timings are milliseconds.
	// wall-clock: half the production floating IP poll (fipPollWait).
	if d := time.Since(start); d > fipPollWait/2 {
		t.Errorf("%s waited %s: a production interval, not the world's Timings", c.tool, d)
	}
	return cloud, text, dir, r
}

// stateText is every file the call wrote under its state directory: the
// deployment state, plans, journals.
func stateText(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(path)
			b.Write(data)
		}
		return nil
	})
	return b.String()
}

// The fault matrix (process audit 1.2.1, second wave, П4): every billable
// tool of the fault world, failed at each step k of its happy path with
// each kind of fault. Two invariants instead of a hand-picked expectation
// per point:
//
//	(a) every billable object left in the cloud is named — by its id in
//	    the answer or in the state the call wrote — so nothing is billed
//	    that the user cannot find;
//	(b) the same call again, the fault gone, converges: it leaves no more
//	    billable objects than one clean call does.
func TestFault_Matrix(t *testing.T) {
	prev := deployplan.SSHPortWait
	deployplan.SSHPortWait = time.Millisecond
	t.Cleanup(func() { deployplan.SSHPortWait = prev })
	for _, c := range faultMatrixCases {
		t.Run(c.tool, func(t *testing.T) {
			happy, text, _, _ := faultRun(t, c, 0, "")
			steps := len(happy.callLog())
			vms, fips := len(happy.liveVMs()), len(happy.liveFIPs())
			if vms == 0 {
				t.Fatalf("the happy path created no VM: %s", text)
			}
			for k := 1; k <= steps; k++ {
				for _, kind := range faultMatrixKinds {
					t.Run(fmt.Sprintf("%d-%s-%s", k, happy.callLog()[k-1], kind), func(t *testing.T) {
						cloud, text, dir, r := faultRun(t, c, k, kind)
						named := text + stateText(t, dir)
						for _, vm := range cloud.liveVMs() {
							if !strings.Contains(named, vm.ID) {
								t.Errorf("(a) VM %s is billed and named nowhere: %s", vm.ID, text)
							}
						}
						for _, f := range cloud.liveFIPs() {
							if !strings.Contains(named, f.ID) && (f.IP == "" || !strings.Contains(named, f.IP)) {
								t.Errorf("(a) floating IP %s is billed and named nowhere: %s", f.ID, text)
							}
						}
						cloud.disarm()
						again, _ := r.handlers[c.tool](context.Background(), c.args)
						if v, f := len(cloud.liveVMs()), len(cloud.liveFIPs()); v > vms || f > fips {
							t.Errorf("(b) the repeat left %d VMs, %d floating IPs; one clean call leaves %d, %d\nfirst: %s\nagain: %s", v, f, vms, fips, text, again)
						}
					})
				}
			}
		})
	}
}
