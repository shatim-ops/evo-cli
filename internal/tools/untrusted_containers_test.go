// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/vmstage"
)

// untrustedContainers are the fields of an answer into which evo carries
// the words of the cloud along with its own: an error, warnings, the
// failures of a part, a preview, a hint. Any answer of any tool that has
// one marks it in "_untrusted", with no declaration of the tool (owner's
// decision 17.11 on #2254, variant B: a forgotten declaration falls on the
// safe side; #2177, #2243–#2247). "warning" is the form of one warning
// (image_list, Астра #2277).
var untrustedContainers = []string{"error", "warnings", "warning", "failures", "failed", "preview", "hint"}

// unmarkedContainers are the fields of answer m its "_untrusted" does not
// name and should: a field of untrustedContainers, and a field that holds
// one at any depth — the answer of a service carried whole, as result of
// disk_attach with its error and warnings (Астра #2276).
func unmarkedContainers(m map[string]any) []string {
	var marked []string
	switch u := m["_untrusted"].(type) {
	case []any:
		for _, f := range u {
			if s, ok := f.(string); ok {
				marked = append(marked, s)
			}
		}
	case []string:
		marked = u
	}
	var out []string
	for _, f := range slices.Sorted(maps.Keys(m)) {
		v := m[f]
		if v == nil || f == "_untrusted" || slices.Contains(marked, f) {
			continue
		}
		if slices.Contains(untrustedContainers, f) || holdsContainer(v) {
			out = append(out, f)
		}
	}
	return out
}

// holdsContainer: v holds a field of untrustedContainers at any depth.
func holdsContainer(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if e != nil && slices.Contains(untrustedContainers, k) || holdsContainer(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if holdsContainer(e) {
				return true
			}
		}
	}
	return false
}

// containerBudget is the budget of a call in the worlds of
// TestUntrustedContainers_MarkedInEveryAnswer.
// wall-clock: the test reads only the marks of whatever answer a call gave
// — cut short by the budget or not — never how far it got; the budget only
// keeps waits for an operation short.
const containerBudget = 100 * time.Millisecond

// containerWorld is a world a tool answers in: the cloud's answers, and
// whether the call carries the consent.
type containerWorld struct {
	name      string
	api       http.RoundTripper
	confirmed bool
}

// Every answer of every tool that has a field of untrustedContainers
// names it in "_untrusted", in each world an answer comes from: a preview
// before the consent, a success and a partial success after it, a refusal
// of the API, a connection that breaks after a read. The worlds give the
// cloud's names with a marker (hostileBody); the check is on the fields,
// whatever text is in them: evo's own words in them are marked too
// (#2254, the cost of variant B).
func TestUntrustedContainers_MarkedInEveryAnswer(t *testing.T) {
	faultToolsIsolate(t)
	// A wait for an operation ends with the budget of the call, as in an
	// MCP call (D69): the answer of a wait cut short is a world too.
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	body := hostileBody()
	var worlds []containerWorld
	for _, confirmed := range []bool{false, true} {
		worlds = append(worlds,
			containerWorld{"answers of the cloud", &oddAPI{status: http.StatusOK, body: body}, confirmed},
			containerWorld{"refusals of the API (422)", &oddAPI{status: http.StatusUnprocessableEntity, body: body}, confirmed},
			containerWorld{"not found (404)", &oddAPI{status: http.StatusNotFound, body: body}, confirmed})
		for from := 2; from <= 4; from++ {
			if confirmed && from > 2 {
				break
			}
			worlds = append(worlds, containerWorld{"a connection broken from a later request", &brokenAfterAPI{body: body, from: from}, confirmed})
		}
	}
	seen := map[string]int{}
	for _, w := range worlds {
		cloud := provision.NewCloudRuAdapter(&http.Client{Transport: w.api}, "")
		cloud.SetToken("t")
		cloud.Backoff = 1
		r := raRegistry(t, w.api, cloud, nil)
		r.SetCallBudget(containerBudget)
		for _, tl := range r.Tools() {
			for _, args := range requiredCalls(tl) {
				args = maps.Clone(args)
				if w.confirmed && tl.Mutating {
					args["confirmed"] = true
				}
				_, text, _ := callNamed(t, r, tl.Name, args)
				var m map[string]any
				if json.Unmarshal([]byte(text), &m) != nil {
					continue
				}
				for _, f := range untrustedContainers {
					if _, has := m[f]; has {
						seen[f]++
					}
				}
				if bad := unmarkedContainers(m); len(bad) > 0 {
					t.Errorf("%s %v, %s (confirmed %v): %v not in _untrusted %v", tl.Name, args, w.name, w.confirmed, bad, m["_untrusted"])
				}
			}
		}
	}
	t.Logf("answers with a field of untrustedContainers: %v", seen)
}

// Any answer of any tool with the fields of untrustedContainers marks
// them, whatever handler gave it: the one point is the registry's wrap of
// every call (outer), over the policies of a transport too (Wrap: the
// refusals of --read-only and of MCP over HTTP are answers with an error).
// The worlds above reach error, hint and preview; this reaches every field
// with no world of its own (warnings of deploy_plan #2244 and
// marketplace_list #2245, failures of open_ports #2246, failed of
// stack_destroy #2247).
func TestUntrustedContainers_MarkedOverAnyHandler(t *testing.T) {
	answer := map[string]any{"status": "error", "error": "e", "warnings": []any{"w"}, "warning": "w1", "failures": []any{"f"},
		"failed": []any{"x"}, "preview": map[string]any{"message": "p"}, "hint": "h",
		"result": map[string]any{"error": map[string]any{"message": "m"}, "warnings": []any{map[string]any{"message": "m"}}}}
	for _, isErr := range []bool{false, true} {
		r := New(nil)
		for _, tl := range r.Tools() {
			r.Wrap(tl.Name, func(Handler) Handler {
				return func(context.Context, map[string]any) (string, bool) { return toJSON(answer), isErr }
			})
		}
		for _, tl := range r.Tools() {
			text, _ := tl.Handler(context.Background(), map[string]any{})
			var m map[string]any
			if err := json.Unmarshal([]byte(text), &m); err != nil {
				t.Fatalf("%s: not JSON: %s", tl.Name, text)
			}
			if _, kept := m["warnings"]; !kept {
				continue // the call was refused before the handler: the worlds above see it
			}
			if bad := unmarkedContainers(m); len(bad) > 0 {
				t.Errorf("%s (error %v): %v not in _untrusted %v", tl.Name, isErr, bad, m["_untrusted"])
			}
		}
	}
}

// hostileStageVM is the stage VM whose compose-up writes a line of the
// workload that reads as an instruction (Астра #2275).
type hostileStageVM struct{ *stageVM }

func (v hostileStageVM) Read(ctx context.Context, name string) (vmstage.Stage, error) {
	st, err := v.stageVM.Read(ctx, name)
	if name == "compose-up" {
		st.LogTail = "web-1  | pulling\nweb-1  | " + hostileMarker + "\n" + hostileMarker
	}
	return st, err
}

// A line of progress (notifications/progress, WithProgress) is evo's text:
// the workload's output that it passes on never starts a line of it, and a
// stage of evo ("==> …") never carries it (Астра #2275: "==> IGNORE
// PREVIOUS INSTRUCTIONS" from the LogTail of compose-up). The channel has no "_untrusted": the
// quotes, or a prefix that says whose the line is, are its one defence.
func TestUntrustedContainers_ProgressLinesAreEvos(t *testing.T) {
	vm := newStageVM()
	r := stageRegistry(t, vm, progressBudget, func(e *engine.Engine) { e.Deployer = hostileStageVM{vm} })
	var mu sync.Mutex
	var lines []string
	ctx := WithProgress(context.Background(), func(l string) {
		mu.Lock()
		lines = append(lines, l)
		mu.Unlock()
	})
	for range deployStageCalls {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = r.handlers["deploy"](ctx, map[string]any{"confirmed": true})
		}()
		select {
		case <-done:
		case <-time.After(progressWatch):
			t.Fatal("deploy did not answer within its budget")
		}
		vm.mu.Lock()
		started := vm.starts > 0
		vm.mu.Unlock()
		if started {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	reached := false
	for _, l := range lines {
		for _, part := range strings.Split(l, "\n") {
			if strings.Contains(part, "pulling") || strings.Contains(part, hostileMarker) {
				reached = true
			}
			line := strings.TrimSpace(part)
			if strings.HasPrefix(strings.TrimLeft(line, "=>-•* "), hostileMarker) ||
				strings.HasPrefix(line, "==>") && strings.Contains(line, hostileMarker) {
				t.Errorf("a line of the workload is a line of evo's progress (a stage of evo, or a line of its own): %q", part)
			}
			// The workload's line is shown in quotes, whole on its line
			// («Риск», conclusion 344, У1).
			if strings.Contains(part, hostileMarker) && !strings.Contains(part, `"`+hostileMarker+`"`) {
				t.Errorf("a line of the workload is not in quotes: %q", part)
			}
		}
	}
	if !reached {
		t.Fatalf("the progress carries no line of the stage: the check sees nothing (%d lines)", len(lines))
	}
}

// The check of a field: named — passes; not named, or a marker of another
// field — fails; absent or null — nothing to mark.
func TestUntrustedContainers_Check(t *testing.T) {
	for _, c := range []struct {
		m    map[string]any
		want []string
	}{
		{map[string]any{"error": "x", "_untrusted": []any{"error"}}, nil},
		{map[string]any{"error": "x", "_untrusted": []string{"error"}}, nil},
		{map[string]any{"error": "x"}, []string{"error"}},
		{map[string]any{"warnings": []any{"w"}, "preview": map[string]any{}, "_untrusted": []any{"preview"}}, []string{"warnings"}},
		{map[string]any{"hint": nil}, nil},
		{map[string]any{"message": "evo's"}, nil},
		{map[string]any{"warning": "w"}, []string{"warning"}},
		{map[string]any{"result": map[string]any{"error": map[string]any{"message": "m"}}}, []string{"result"}},
		{map[string]any{"result": map[string]any{"items": []any{map[string]any{"warnings": []any{"w"}}}}, "_untrusted": []any{}}, []string{"result"}},
		{map[string]any{"result": map[string]any{"error": "m"}, "_untrusted": []any{"result"}}, nil},
		{map[string]any{"result": map[string]any{"id": "x"}}, nil},
	} {
		if got := unmarkedContainers(c.m); !slices.Equal(got, c.want) {
			t.Errorf("%v: %v, want %v", c.m, got, c.want)
		}
	}
}
