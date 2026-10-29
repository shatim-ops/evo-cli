// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// refusedToken is the error a client gives for a 401 to a token evo still
// holds valid: the machine slept, the clock of the paused VM is behind
// (G140-3). evo has dropped the token; the next request takes a new one.
func refusedToken() error {
	return clerrors.New(clerrors.CodeAuthExpired, "Cloud.ru API rejected the token (401)", "repeat the command")
}

// matrixRead and matrixWrite tell the calls of a fault world apart: the
// compute world logs the method (GetVM), the API world the request
// ("GET host/path").
func matrixRead(call string) bool {
	return strings.HasPrefix(call, "GET ") || strings.HasPrefix(call, "Get") || strings.HasPrefix(call, "List")
}

func matrixWrite(call string) bool {
	for _, p := range []string{"POST ", "PUT ", "PATCH ", "DELETE ", "Create", "Attach", "Delete", "Update", "Add"} {
		if strings.HasPrefix(call, p) {
			return true
		}
	}
	return false
}

// oneRefusalEndsTool are the reads after a write where one refused token
// still ends the tool's call, "tool<TAB>call" → why; a line whose read
// goes on fails the test. The CLI's deploy is engine's oneRefusalEnds.
var oneRefusalEndsTool = map[string]string{}

// The machine sleeps in the middle of a billable call of the fault matrix
// (CLAUDE.md §6, axes 2 and 3; process backlog 1.6, priority 1): one read
// after the first write — the wait for what the call made, its check —
// meets a token the service refuses once. The call goes on: the resource
// is there and billed, and an error answer would read as «not made».
func TestFault_OneRefusedTokenAfterTheWrite(t *testing.T) {
	prev := deployplan.SSHPortWait
	deployplan.SSHPortWait = time.Millisecond
	t.Cleanup(func() { deployplan.SSHPortWait = prev })
	seen := map[string]bool{}
	reads := 0
	for _, c := range faultMatrixCases {
		happy, _, _, _ := faultRun(t, c, 0, "")
		calls := happy.callLog()
		wrote := false
		for k := 1; k <= len(calls); k++ {
			call := calls[k-1]
			if matrixWrite(call) {
				wrote = true
				continue
			}
			if !wrote || !matrixRead(call) {
				continue
			}
			reads++
			key := c.tool + "\t" + call
			seen[key] = true
			_, text, _, _ := faultRun(t, c, k, faultAuth)
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			failed := m["status"] == "error"
			if why, declared := oneRefusalEndsTool[key]; declared {
				if !failed {
					t.Errorf("%s: in oneRefusalEndsTool (%s), but the call goes on; take the line out", strings.Replace(key, "\t", " ", 1), why)
				}
				continue
			}
			if failed {
				t.Errorf("%s (call %d): one refused token after the write ends the call — %s", strings.Replace(key, "\t", " ", 1), k, text)
			}
		}
	}
	if reads == 0 {
		t.Fatal("no case of the matrix reads after its first write: the test checks nothing")
	}
	for key := range oneRefusalEndsTool {
		if !seen[key] {
			t.Errorf("oneRefusalEndsTool names %q, a read no case makes after a write: remove the line", key)
		}
	}
}

// sleptDiskCloud lists the new disk creating, then refuses the token once
// (the machine slept), then lists it available.
type sleptDiskCloud struct {
	cancelCloud
	mu    sync.Mutex
	polls int
}

func (c *sleptDiskCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.polls++
	switch c.polls {
	case 1:
		return []map[string]any{{"id": "d-1", "state": "creating"}}, nil
	case 2:
		return nil, refusedToken()
	}
	return []map[string]any{{"id": "d-1", "state": "available"}}, nil
}

// The wait for a new disk before its attach (waitDiskAvailable) meets the
// refused token after the sleep and goes on to the disk available.
func TestDiskWait_GoesOnAfterOneRefusedToken(t *testing.T) {
	cloud := &sleptDiskCloud{}
	eng := &engine.Engine{Cloud: cloud, Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: time.Second}}
	if err := waitDiskAvailable(context.Background(), eng, "d-1", "p"); err != nil {
		t.Fatalf("the disk wait ends at the refused token: %v", err)
	}
	if cloud.polls < 3 {
		t.Fatalf("the world did not reach the refusal (%d polls): it checks nothing", cloud.polls)
	}
}

// refusedOnce answers a refused token on its first call, then v.
func refusedOnce(v map[string]any) (Handler, *int) {
	var mu sync.Mutex
	calls := 0
	return func(context.Context, map[string]any) (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return errResult(refusedToken()), true
		}
		return toJSON(v), false
	}, &calls
}

// stack_status advances a job whose steps wait — one for an operation
// (vpc), one for a status (postgres) — while the machine sleeps: the first
// poll of each after the sleep meets the refused token. The job goes on to
// done, no step fails, nothing is created again.
func TestStackStatus_GoesOnAfterOneRefusedToken(t *testing.T) {
	stackHome(t)
	prev := stackCrankInterval
	stackCrankInterval = time.Millisecond
	t.Cleanup(func() { stackCrankInterval = prev })
	r := newTestServer()
	j := planJob(t, `{"name":"w","vpc":{"name":"v"},"postgres":{"name":"p"}}`)
	j.Status = "running"
	for _, st := range j.Steps {
		st.Status = "waiting"
		switch st.Key {
		case "vpc":
			st.OpID, st.ResourceID = "op-1", "vpc-1"
		case "postgres":
			st.ResourceID = "pg-1"
		}
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	var creates int
	create := func(context.Context, map[string]any) (string, bool) {
		creates++
		return toJSON(map[string]any{}), false
	}
	r.handlers["vpc_create"], r.handlers["pg_create"] = create, create
	op, opCalls := refusedOnce(map[string]any{"id": "op-1", "done": true})
	pg, pgCalls := refusedOnce(map[string]any{"id": "pg-1", "status": "READY"})
	r.handlers["vpc_operation"], r.handlers["pg_get"] = op, pg
	got := callTool(t, r.handleStackStatus, map[string]any{"job_id": j.ID, "wait_seconds": float64(5)})
	if *opCalls < 2 || *pgCalls < 2 {
		t.Fatalf("the world did not reach the refusal (vpc_operation %d, pg_get %d): it checks nothing — %v", *opCalls, *pgCalls, got)
	}
	if got["status"] != "done" || creates != 0 {
		t.Errorf("one refused token on the polls of waiting steps: status %v, creates again %d, want done and 0 — %v", got["status"], creates, got)
	}
}
