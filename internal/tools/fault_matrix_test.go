// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// faultWorld is a fake cloud a matrix case runs in: it counts the calls,
// fails the armed one, and keeps what was made when an answer is lost.
type faultWorld interface {
	failAt(n int, kind string)
	disarm()
	callLog() []string
	// billed are the billable objects alive in the world, each with the
	// strings that name it.
	billed() []faultBilled
}

type faultBilled struct {
	what  string
	names []string
}

// computeWorld is faultCloud as a matrix world: VMs by id, floating IPs by
// id or address.
type computeWorld struct{ *faultCloud }

func (w computeWorld) billed() []faultBilled {
	var out []faultBilled
	for _, vm := range w.liveVMs() {
		out = append(out, faultBilled{what: "VM " + vm.ID, names: []string{vm.ID}})
	}
	for _, f := range w.liveFIPs() {
		names := []string{f.ID}
		if f.IP != "" {
			names = append(names, f.IP)
		}
		out = append(out, faultBilled{what: "floating IP " + f.ID, names: names})
	}
	return out
}

// faultMatrixCase is a billable tool whose cloud a fault world models.
type faultMatrixCase struct {
	tool string
	args map[string]any
	// world makes a fresh world and the registry over it; cancel is the
	// call's context cancel, which faultCancel fires. dir is the state
	// directory the call writes, if any.
	world func(t *testing.T, cancel context.CancelFunc) (w faultWorld, r *Registry, dir string)
	// skip leaves out a fault at a call: a reason, or "".
	skip func(call, kind string) string
}

// computeCase is a case of the compute world (faultCloud).
func computeCase(tool string, args map[string]any, noDial bool, setup func(t *testing.T, dir string)) faultMatrixCase {
	return faultMatrixCase{tool: tool, args: args, world: func(t *testing.T, cancel context.CancelFunc) (faultWorld, *Registry, string) {
		cloud := &faultCloud{noDial: noDial, cancel: cancel}
		r, dir := faultToolsRegistry(t, cloud)
		if setup != nil {
			setup(t, dir)
		}
		return computeWorld{cloud}, r, dir
	}}
}

// apiCase is a case of the HTTP world (faultAPI).
func apiCase(tool string, args map[string]any) faultMatrixCase {
	return faultMatrixCase{tool: tool, args: args,
		world: func(t *testing.T, cancel context.CancelFunc) (faultWorld, *Registry, string) {
			a := &faultAPI{cancel: cancel}
			return a, faultAPIRegistry(t, a), ""
		},
		skip: func(call, kind string) string {
			if (kind == faultTransport || kind == faultLost) && strings.HasPrefix(call, http.MethodGet+" ") {
				return "httpretry repeats a read after its 1 s backoff (internal/httpretry tests)"
			}
			return ""
		}}
}

var faultMatrixCases = []faultMatrixCase{
	computeCase("provision", map[string]any{"name": "web", "confirmed": true}, false, nil),
	computeCase("deploy_apply", map[string]any{"plan_id": "plan-0123456789ab", "confirmed": true}, true,
		func(t *testing.T, dir string) {
			if err := deployplan.Save(dir, faultPlan()); err != nil {
				t.Fatal(err)
			}
		}),
	apiCase("kafka_create", map[string]any{"name": "evo-k", "cpu": 2, "ram": 4, "confirmed": true}),
	apiCase("redis_create", map[string]any{"name": "evo-r", "cpu": 2, "ram": 4, "confirmed": true}),
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
// kind (k == 0: no failure). It returns the world, the answer, the state
// directory and the registry.
func faultRun(t *testing.T, c faultMatrixCase, k int, kind string) (faultWorld, string, string, *Registry) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w, r, dir := c.world(t, cancel)
	if k > 0 {
		w.failAt(k, kind)
	}
	start := time.Now()
	text, _ := r.handlers[c.tool](ctx, c.args)
	// The world's Timings are milliseconds.
	// wall-clock: half the production floating IP poll (fipPollWait).
	if d := time.Since(start); d > fipPollWait/2 {
		t.Errorf("%s waited %s: a production interval, not the world's Timings", c.tool, d)
	}
	return w, text, dir, r
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
			calls := happy.callLog()
			clean := len(happy.billed())
			if clean == 0 {
				t.Fatalf("the happy path made nothing billable: %s", text)
			}
			for k := 1; k <= len(calls); k++ {
				for _, kind := range faultMatrixKinds {
					if c.skip != nil && c.skip(calls[k-1], kind) != "" {
						continue
					}
					t.Run(fmt.Sprintf("%d-%s-%s", k, calls[k-1], kind), func(t *testing.T) {
						w, text, dir, r := faultRun(t, c, k, kind)
						named := text
						if dir != "" {
							named += stateText(t, dir)
						}
						for _, b := range w.billed() {
							if !namedBy(named, b.names) {
								t.Errorf("(a) %s is billed and named nowhere: %s", b.what, text)
							}
						}
						w.disarm()
						again, _ := r.handlers[c.tool](context.Background(), c.args)
						if n := len(w.billed()); n > clean {
							t.Errorf("(b) the repeat left %d billable objects; one clean call leaves %d\nfirst: %s\nagain: %s", n, clean, text, again)
						}
					})
				}
			}
		})
	}
}

func namedBy(text string, names []string) bool {
	for _, n := range names {
		if strings.Contains(text, n) {
			return true
		}
	}
	return false
}
