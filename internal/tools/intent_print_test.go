// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// consentedCall is a call of tool with consent through the layer of
// intents, its handler answering answer.
func consentedCall(r *Registry, tool string, args map[string]any, answer string) {
	tl, _ := r.Lookup(tool)
	args["confirmed"] = true
	r.intended(tl, func(context.Context, map[string]any) (string, bool) {
		return answer, strings.Contains(answer, `"error"`)
	})(context.Background(), args)
}

// The record of a call names the call, not only the resource it is made
// on: a call that changes a part of its parent — a rule of a security
// group — left a record named by the group alone, and any other rule of
// the same group, once answered, closed it: the cut rule «22 from
// anywhere» was forgotten after a rule on port 80 (the interim delta audit
// of 1.9, FB19I-13; «Риск», conclusion 177, У4). The same call is the same
// tool, project, key and arguments — without the consent, the wait and the
// values that are secrets; a record keeps no value of an argument.
func TestIntent_AnotherCallOnTheSameResourceDoesNotSettleTheRecord(t *testing.T) {
	const cut = `{"status":"error","reason":"API_UNREACHABLE"}`
	const done = `{"status":"created"}`
	rule := func(port string, more ...any) map[string]any {
		a := map[string]any{"security_group": resMine, "ports": port, "cidr": "0.0.0.0/0"}
		for i := 0; i+1 < len(more); i += 2 {
			a[more[i].(string)] = more[i+1]
		}
		return a
	}
	// open_ports: a tool that makes rules of a group and is not one of
	// everySendIsAChange — a call of those after a cut one is asked again
	// whatever its arguments (the test below), and its question closes the
	// record; sg_rule_create became one with FB19I-12.
	const tool = "open_ports"
	if everySendIsAChange[tool].read != "" {
		t.Fatalf("%s is a tool every send of which is a change: its question closes the record, take another", tool)
	}
	for _, c := range []struct {
		name  string
		again map[string]any
		left  int
	}{
		{"the same call", rule("22"), 0},
		{"the same call that waits otherwise", func() map[string]any { a := rule("22"); a["wait"] = false; a["wait_seconds"] = 5.0; return a }(), 0},
		{"another rule of the same group", rule("80"), 1},
		{"the same rule with one more argument", rule("22", "allow_internet_on_shared_group", true), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ownerRegistry(t, &ownerFake{}, false, ownProject)
			if _, ok := r.Lookup(tool); !ok {
				t.Fatalf("no tool %s", tool)
			}
			eng, _ := r.newEngine(nil)
			dir := intent.Dir(eng.StateDir)
			consentedCall(r, tool, rule("22"), cut)
			was := intent.Pending(dir)
			if len(was) != 1 || was[0].Print == "" || !was[0].Ended {
				t.Fatalf("the record of the cut call: %+v", was)
			}
			// No value of an argument is in the file but the resource.
			data, _ := os.ReadFile(dir + "/" + was[0].ID + ".json")
			if strings.Contains(string(data), "0.0.0.0") || strings.Contains(string(data), `"22"`) {
				t.Errorf("the record holds a value of an argument:\n%s", data)
			}
			consentedCall(r, tool, c.again, done)
			if left := intent.Pending(dir); len(left) != c.left {
				t.Errorf("%d records left, want %d: %+v", len(left), c.left, left)
			}
		})
	}
	// A secret is not in the print: two calls that differ in it alone are
	// one call, as before the print, and the print of the record does not
	// change with the secret.
	t.Run("a secret is not in the print", func(t *testing.T) {
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		tl, _ := r.Lookup("secret_add_version")
		if !tl.CLI.SecretFiles["value"] {
			t.Fatal("secret_add_version declares no secret argument")
		}
		a := r.canonArgs("secret_add_version", map[string]any{"path": "db/pw", "value": "one"})
		b := r.canonArgs("secret_add_version", map[string]any{"path": "db/pw", "value": "two", "confirmed": true})
		if string(a) != string(b) || strings.Contains(string(a), "one") {
			t.Errorf("the canonical arguments hold the secret: %s, %s", a, b)
		}
	})
}

// A consented call writes its record whenever it may reach the cloud: on
// a server bound to projects whose own project is not among them
// (--allow-project), no engine is built without arguments — and the record
// was not written, though the call with project_id went out (the interim
// delta audit of 1.9, FB19I-32; «Риск», conclusion 177, У9). The record is
// of the project the call goes with. A call no engine is built for at all
// is recorded without a key, and no repeat settles such a record.
func TestIntent_ACallOfAServerWithoutItsOwnProjectIsRecorded(t *testing.T) {
	const other = "00000000-0000-0000-0000-0000000000b2"
	r := ownerRegistry(t, &ownerFake{}, true, other) // the profile's project is not allowed
	if eng, err := r.newEngine(nil); err == nil || eng != nil {
		t.Fatal("the world builds an engine without a project: it is not the server of the finding")
	}
	root, _ := r.stateDir()
	dir := intent.Dir(root)
	tl, _ := r.Lookup("disk_create")
	during := -1
	args := func(project string) map[string]any {
		a := map[string]any{"name": "data", "size_gb": 10.0, "confirmed": true}
		if project != "" {
			a["project_id"] = project
		}
		return a
	}
	call := func(project, answer string) {
		r.intended(tl, func(context.Context, map[string]any) (string, bool) {
			during = len(intent.Pending(dir))
			return answer, strings.Contains(answer, `"error"`)
		})(context.Background(), args(project))
	}
	call(other, `{"status":"error","reason":"API_ERROR"}`)
	left := intent.Pending(dir)
	if during != 1 || len(left) != 1 || left[0].ProjectID != other || left[0].Key == nil {
		t.Fatalf("a call with project_id on a server without its own project: %d records during it, after an unknown outcome %+v", during, left)
	}
	// The same call, answered, settles it.
	call(other, `{"status":"created"}`)
	if left := intent.Pending(dir); len(left) != 0 {
		t.Fatalf("the repeat did not settle the record: %+v", left)
	}
	// No engine at all: the record is written without a key, and a
	// repeat does not settle it.
	call("", `{"status":"error","reason":"API_ERROR"}`)
	left = intent.Pending(dir)
	if during != 1 || len(left) != 1 || left[0].Key != nil || left[0].Print == "" {
		t.Fatalf("a call no engine is built for: %d records during it, after it %+v", during, left)
	}
	call("", `{"status":"created"}`)
	if left := intent.Pending(dir); len(left) != 1 {
		t.Errorf("a repeat settled a record whose key is not known: %+v", left)
	}
	if notes := r.PendingNotes(root); len(notes) != 1 {
		t.Errorf("the record without a key is not named: %v", notes)
	}
}

// The print is the rule of closing a record, not the rule of the question:
// after a cut call of a tool every send of which is a change, a call on
// the same resource asks again whatever its other arguments are —
// otherwise the question would be passed round by changing an argument
// that does not matter («Риск», conclusion 177, У12).
func TestIntent_TheQuestionAfterACutCallDoesNotLookAtThePrint(t *testing.T) {
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, _ := r.newEngine(nil)
	dir := intent.Dir(eng.StateDir)
	asked := 0
	for tool := range everySendIsAChange {
		if len(idArgs[tool]) == 0 {
			continue // named by what it makes, not by a resource
		}
		asked++
		target := idArgs[tool][0].name
		consentedCall(r, tool, map[string]any{target: resMine}, `{"status":"error","reason":"API_UNREACHABLE"}`)
		sent := 0
		tl, _ := r.Lookup(tool)
		text, _ := r.intended(tl, func(context.Context, map[string]any) (string, bool) {
			sent++
			return `{"status":"ok"}`, false
		})(context.Background(), map[string]any{target: resMine, "wait": false, "description": "another", "confirmed": true})
		if sent != 0 || !strings.Contains(text, `"interrupted"`) {
			t.Errorf("%s with other arguments after its cut call: sent %d\n%s", tool, sent, text)
		}
		for _, in := range intent.Pending(dir) {
			intent.Close(in)
		}
	}
	if asked == 0 {
		t.Error("no tool every send of which is a change names a resource")
	}
}
