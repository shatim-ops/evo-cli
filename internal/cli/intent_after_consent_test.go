// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// intentFiles are the files of intent of root (intent.Dir): what a
// consented call that the process did not finish leaves.
func intentFiles(root string) []string {
	m, _ := filepath.Glob(filepath.Join(intent.Dir(root), "*.json"))
	sort.Strings(m)
	return m
}

// atFirstWrite is what the directory of the state held when the cloud got
// the first write of a call.
type atFirstWrite struct {
	mu    sync.Mutex
	seen  bool
	files map[string]string // path → content
}

func (a *atFirstWrite) reset() {
	a.mu.Lock()
	a.seen, a.files = false, nil
	a.mu.Unlock()
}

// intentKeeps are the arguments whose value an intent holds by design:
// the resource, the project and the zone it names (package intent).
var intentKeeps = map[string]bool{"name": true, "project_id": true, "zone": true, "availability_zone_id": true}

// argValues are the values of the arguments a file of intent must not
// hold: the strings of four and more characters (a short one — "1",
// "HDD" — is also the text of a field) but those of intentKeeps.
func argValues(args map[string]any) []string {
	var out []string
	for k, v := range args {
		if s, ok := v.(string); ok && len(s) >= 4 && !intentKeeps[k] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// intentRandom are the fields of a record whose text evo makes itself of
// chance and of the clock — the id, the hash of the call (Print: the
// values are not written into it), the start, the process. A value of
// four characters («8080», «icmp») is found in such hex and digits by
// chance: 1 of 158 calls of a CI run, «evo vpc sg-rule-create», 13.11,
// after the print came (#1620). Every other field — and one added later
// — is read for the values.
var intentRandom = []string{"id", "print", "started_at", "pid"}

// intentSays is the record without intentRandom: the text an argument's
// value is looked for in.
func intentSays(t *testing.T, body string) string {
	t.Helper()
	var rec map[string]any
	if err := json.Unmarshal([]byte(body), &rec); err != nil {
		return body
	}
	for _, k := range intentRandom {
		if _, ok := rec[k]; !ok {
			t.Errorf("the record has no field %q: intentRandom names a field the layer no longer writes", k)
		}
		delete(rec, k)
	}
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// intentSays leaves out chance and nothing else: a value in any other
// field of the record is still found.
func TestGate_IntentSaysLeavesOutOnlyChance(t *testing.T) {
	body := `{"id":"a8443b","tool":"sg_rule_create","args":["ports"],"name":"web","note":"x 8443 y","surface":"mcp","pid":18443,"started_at":"2026-11-13T07:08:08.084430Z","print":"ff8443aa"}`
	if !strings.Contains(intentSays(t, body), "8443") {
		t.Error("a value in a field evo does not make of chance is not seen")
	}
	if strings.Contains(intentSays(t, strings.Replace(body, `"note":"x 8443 y",`, "", 1)), "8443") {
		t.Error("the id, the print, the start or the process is still read for the values")
	}
}

// R-59 (round 3 of Grok, F1; cloud-e2e 181, R3F1 1 of 2): a SIGKILL of evo
// after «Confirmed» and before the cloud answers left a paid disk in the
// cloud and nothing in evo — the journal writes a call when it returns.
// Decision of the owner 10.11 (plan 1.8, B-5): the gate D35 writes the
// intent before it executes and removes it on the answer. Every consented
// call of the JSON world, on both surfaces: when the cloud gets the call's
// first write, a file of intent is in intent.Dir, names the tool, the
// surface and the process and holds no value of an argument but the
// resource, the project and the zone; when the call has answered, it is gone. A call refused before
// consent leaves none.
func TestGate_IntentBeforeTheCloudGoneAfter(t *testing.T) {
	w, fake := withJSONWorld(t)
	seen := &atFirstWrite{}
	inner := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			seen.mu.Lock()
			if !seen.seen {
				seen.seen = true
				seen.files = map[string]string{}
				for _, p := range intentFiles(w.dir) {
					b, _ := os.ReadFile(p)
					seen.files[p] = string(b)
				}
			}
			seen.mu.Unlock()
		}
		return inner(r)
	}
	cases, _, _ := jsonCases(t)
	found := map[string][]string{}
	note := func(key, f string, a ...any) { found[key] = append(found[key], fmt.Sprintf(f, a...)) }
	checked := 0
	for _, c := range cases {
		if !c.tool.Mutating || !tools.NeedsConsent(c.tool, c.args) {
			continue
		}
		key := c.path
		if c.state != "" {
			key += " (" + c.state + ")"
		}
		for _, surface := range []string{"cli", "mcp"} {
			args := copyArgs(c.args)
			done := func() {}
			if c.setup != nil {
				extra, teardown := c.setup(t, w)
				for k, v := range extra {
					args[k] = v
				}
				done = teardown
			}
			fake.reset()
			seen.reset()
			newProcess()
			if surface == "cli" {
				line := c.line
				if line == nil {
					cmd := commandsByTool()[c.tool.Name]
					line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
				}
				runCLI(t, c.stdin, append([]string{"--json", "--yes"}, line...)...)
			} else {
				flagJSON, flagDryRun, flagYes = false, false, false
				a := copyArgs(args)
				a["confirmed"] = true
				tl, _ := newRegistry().Lookup(c.tool.Name)
				tl.Handler(context.Background(), a)
			}
			done()
			seen.mu.Lock()
			wrote, files := seen.seen, seen.files
			seen.mu.Unlock()
			if left := intentFiles(w.dir); len(left) > 0 {
				note(key, "%s: the call answered, and the intent is still there: %v", surface, left)
				for _, p := range left {
					_ = os.Remove(p)
				}
			}
			forgetRun(w)
			if !wrote {
				continue // nothing reached the cloud: nothing to leave behind
			}
			checked++
			if len(files) == 0 {
				note(key, "%s: the cloud got the first write, and no intent was in the directory of the state", surface)
				continue
			}
			for p, body := range files {
				var rec intent.Intent
				if err := json.Unmarshal([]byte(body), &rec); err != nil {
					note(key, "%s: %s is not an intent: %v", surface, filepath.Base(p), err)
					continue
				}
				if rec.Tool != c.tool.Name {
					note(key, "%s: the intent names the tool %q", surface, rec.Tool)
				}
				if rec.PID != os.Getpid() {
					note(key, "%s: the intent names the process %d, not this one", surface, rec.PID)
				}
				if rec.Surface != surface {
					note(key, "%s: the intent names the surface %q", surface, rec.Surface)
				}
				// The id of the resource a call on an existing one names
				// (Target, «Риск», conclusion 46, condition 2: no secret) and
				// the declared name argument are where to look.
				named := map[string]bool{rec.Name: true}
				if _, id, ok := strings.Cut(rec.Target, "="); ok {
					named[id] = true
				}
				said := intentSays(t, body)
				for _, v := range argValues(args) {
					if strings.Contains(said, v) && !named[v] {
						note(key, "%s: the intent holds the value of an argument (%d characters)", surface, len(v))
					}
				}
			}
		}
	}
	// Refused before consent: the preview of every such call, no file.
	for _, c := range cases {
		if !c.tool.Mutating || !tools.NeedsConsent(c.tool, c.args) || c.setup != nil {
			continue
		}
		newProcess()
		flagJSON, flagDryRun, flagYes = false, false, false
		tl, _ := newRegistry().Lookup(c.tool.Name)
		tl.Handler(context.Background(), copyArgs(c.args))
		if left := intentFiles(w.dir); len(left) > 0 {
			note(c.path, "the preview, without consent, left an intent: %v", left)
			for _, p := range left {
				_ = os.Remove(p)
			}
		}
	}
	t.Logf("%d consented calls that wrote to the cloud checked", checked)
	if checked < 10 {
		t.Fatalf("%d consented calls reached the cloud: the test proves little", checked)
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, m := range found[k] {
			t.Errorf("%s: %s", k, m)
		}
	}
}
