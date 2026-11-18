// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The repeat of a create whose step goes on (made/) runs that step only
// when it is a read: a step that writes — disk_attach of disk_create with
// attach_to — is named, with no consent in its arguments, and not sent.
// The call that repeats has not consented: nothing is written for it
// (A19-1, the release audit of 1.9 rc.1, #2418).
func TestMadeRepeat_AStepThatWritesIsNotRunWithoutConsent(t *testing.T) {
	a := &faultAPI{slow: true, lag: 1000}
	r := progressed(budgetAPIRegistry(t, a))
	args := map[string]any{"name": "data", "size_gb": 10, "attach_to": faultTargetVM, "confirmed": true}
	m, text, _ := callNamed(t, r, "disk_create", args)
	if m["next_tool"] != "disk_attach" {
		t.Fatalf("the world no longer stops disk_create at the limit with disk_attach next: %s", text)
	}
	// The file of the create keeps no consent: the next call gives its own.
	root, _ := r.stateDir()
	files, _ := filepath.Glob(filepath.Join(MadeDir(root), "*.json"))
	if len(files) != 1 {
		t.Fatalf("want the file of the create going on, got %v", files)
	}
	if b, _ := os.ReadFile(files[0]); strings.Contains(string(b), "confirmed") {
		t.Errorf("the file of made/ keeps the consent of the call: %s", b)
	}
	// The disk is ready now: a step that runs would attach it.
	a.mu.Lock()
	a.slow = false
	a.mu.Unlock()
	delete(args, "confirmed")
	before := len(a.callLog())
	m, text, _ = callNamed(t, r, "disk_create", args)
	for _, op := range a.callLog()[before:] {
		if !strings.HasPrefix(op, "GET ") {
			t.Errorf("the repeat without consent wrote: %s (%v)\n%s", op, a.callLog()[before:], text)
		}
	}
	if m["status"] == "attached" {
		t.Errorf("the repeat without consent answers attached: %s", text)
	}
	if m["next_tool"] != "disk_attach" {
		t.Errorf("the repeat does not name the step that is left: %s", text)
	}
	if next, _ := m["next_args"].(map[string]any); next["confirmed"] != nil {
		t.Errorf("the step the repeat names carries a consent this call did not give: %v", next)
	}
}

// A file of made/ is evo's own only when it names the key that wrote it:
// a file with an empty key — one a cloned directory of the state brings,
// or anyone who writes in it — is not read. A file of the key itself is
// read, and its step still runs only as a read: a restore with confirmed
// true in the file is not sent for a call that did not consent (A19-1,
// #2418, path 2). Each layer is checked on its own.
func TestMadeRepeat_APlantedFileWritesNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		// own: the file names the key of the call; else key, a key that
		// names no field the key of the call knows too.
		own bool
		key string
	}{{"empty key", false, `{}`}, {"an account the key of the call does not name", false, `{"service_account_id":"sa-other"}`}, {"the key of the call", true, ""}} {
		w := (&restoreWorld{}).stateIn(t)
		r := restoreRegistry(t, w)
		args := map[string]any{"backup_id": brBackup, "name": "restored"}
		resource, target, err := r.resourceOf(context.Background(), "backup_restore", args)
		if err != nil {
			t.Fatal(err)
		}
		eng, _ := r.engineOfCall(args)
		project := projectOfCall(eng, intent.Intent{Tool: "backup_restore", Name: resource, Target: target}, args)
		key := []byte(c.key)
		if c.own {
			key, _ = json.Marshal(eng.Maker())
		}
		dir := MadeDir(w.dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		body := `{"key":` + string(key) + `,"id":"x","project_id":"` + project + `","started_at":"` + clock.Now().UTC().Format(time.RFC3339) +
			`","next":"backup_restore","next_args":{"backup_id":"` + brBackup + `","name":"evil-disk","confirmed":true}}`
		file := filepath.Join(dir, madeKey("backup_restore", r.canonArgs("backup_restore", args), resource, target))
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		m, text, _ := callNamed(t, r, "backup_restore", args)
		if w.restores != 0 {
			t.Errorf("%s: a call without consent sent %d restores from the file of made/: %s", c.name, w.restores, text)
		}
		_, read := m["repeat_of"]
		if read != c.own {
			t.Errorf("%s: the file of made/ read: %v, want %v: %s", c.name, read, c.own, text)
		}
		if next, _ := m["next_args"].(map[string]any); c.own && next["confirmed"] != nil {
			t.Errorf("%s: the step named carries the consent of the file: %v", c.name, next)
		}
		// The step that writes is not run at all — not even to its own
		// preview, which would answer confirmation_required: the repeat
		// waits. Its message tells no consent to give: the step asks for
		// it itself («Риск», conclusion 379).
		if c.own && m["status"] != "waiting" {
			t.Errorf("%s: the repeat ran the step that writes (status %v): %s", c.name, m["status"], text)
		}
		if msg, _ := m["message"].(string); c.own && strings.Contains(msg, "confirmed") {
			t.Errorf("%s: the message of the repeat tells the consent to give: %q", c.name, msg)
		}
	}
}

// madeStepWriters is how many pairs of a create and a step the world below
// reaches with a file of made/ read (18.11): a world that reaches fewer
// checks less.
const madeStepWriters = 60

// The class (A19-1, #2418; «Риск»): for every create, a file of made/ of
// the key of the call whose step is a write with confirmed true — the
// create itself — sends no write for a call without consent. The CLI's
// --dry-run and a run without --yes call the same handler without
// consent (invokeTool), so this holds for them too.
func TestMadeRepeat_NoCreateRunsAWriteStepWithoutConsent(t *testing.T) {
	a := &faultAPI{}
	r := budgetAPIRegistry(t, a)
	root, _ := r.stateDir()
	dir := MadeDir(root)
	read := map[string]bool{}
	for name := range allCreated() {
		tl, ok := r.Lookup(name)
		if !ok {
			continue
		}
		for _, args := range requiredCalls(tl) {
			resource, target, err := r.resourceOf(context.Background(), name, args)
			if err != nil || (resource == "" && target == "") {
				continue
			}
			eng, _ := r.engineOfCall(args)
			if eng == nil {
				continue
			}
			project := projectOfCall(eng, intent.Intent{Tool: name, Name: resource, Target: target}, args)
			key, _ := json.Marshal(eng.Maker())
			// Two steps that take consent: the create itself, a write; and
			// secret_value, a read that reveals a secret — each with the
			// consent the file claims.
			for _, next := range []struct {
				tool string
				args map[string]any
			}{{name, maps.Clone(args)}, {"secret_value", map[string]any{"path": "app/db"}}} {
				next.args["confirmed"] = true
				body, _ := json.Marshal(map[string]any{"tool": name, "project_id": project, "id": "x", "next": next.tool, "next_args": next.args,
					"started_at": clock.Now().UTC(), "key": json.RawMessage(key)})
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(dir, madeKey(name, r.canonArgs(name, args), resource, target))
				if err := os.WriteFile(file, body, 0o600); err != nil {
					t.Fatal(err)
				}
				before := len(a.callLog())
				m, text, _ := callNamed(t, r, name, maps.Clone(args))
				for _, op := range a.callLog()[before:] {
					if !strings.HasPrefix(op, "GET ") || strings.Contains(op, "secretmanager") {
						t.Errorf("%s, step %s: a call without consent ran it with a file of made/: %s\n%.300s", name, next.tool, op, text)
						break
					}
				}
				if _, has := m["repeat_of"]; has {
					read[name+" "+next.tool] = true
					// Not run at all — not even to the step's own
					// preview: the repeat waits («Риск», conclusion 379).
					if m["status"] != "waiting" {
						t.Errorf("%s, step %s: the repeat ran the step (status %v)", name, next.tool, m["status"])
					}
				}
				_ = os.Remove(file)
			}
		}
	}
	t.Logf("creates whose file of made/ was read: %d", len(read))
	if len(read) < madeStepWriters {
		t.Errorf("%d creates read a file of made/, %d expected: the world no longer reaches them", len(read), madeStepWriters)
	}
}

// A file of made/ of the key of the call runs as its step only a read of
// the resource the create makes — its list, or a read of the same group
// that names a resource by its id and holds no secret: any other read the
// file names (history, deploy_plan with a path of the machine) is named,
// not run, and its answer does not stand for the repeat's (A19-42, #2470).
func TestMadeRepeat_AStepOfAnotherKindIsNotRun(t *testing.T) {
	for _, next := range []struct {
		tool string
		args string
		// field is a field only the answer of the step has.
		field string
	}{{"history", `{}`, "journal_of"}, {"deploy_plan", `{"compose_file":"/etc/passwd"}`, "services"}} {
		w := (&restoreWorld{}).stateIn(t)
		r := restoreRegistry(t, w)
		args := map[string]any{"backup_id": brBackup, "name": "restored"}
		resource, target, err := r.resourceOf(context.Background(), "backup_restore", args)
		if err != nil {
			t.Fatal(err)
		}
		eng, _ := r.engineOfCall(args)
		project := projectOfCall(eng, intent.Intent{Tool: "backup_restore", Name: resource, Target: target}, args)
		key, _ := json.Marshal(eng.Maker())
		body := `{"key":` + string(key) + `,"id":"x","project_id":"` + project + `","started_at":"` + clock.Now().UTC().Format(time.RFC3339) +
			`","next":"` + next.tool + `","next_args":` + next.args + `}`
		dir := MadeDir(w.dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, madeKey("backup_restore", r.canonArgs("backup_restore", args), resource, target)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		m, text, _ := callNamed(t, r, "backup_restore", args)
		if _, read := m["repeat_of"]; !read {
			t.Fatalf("%s: the world no longer reads the file of made/: %s", next.tool, text)
		}
		if _, ran := m[next.field]; ran || m["status"] != "waiting" {
			t.Errorf("%s: the repeat ran a step that reads another thing than the resource (status %v): %.400s", next.tool, m["status"], text)
		}
	}
}

// madeSteps is what a file of made/ may run as the step of each create: a
// snapshot of madeStepReads over the registry (18.11; «Риск», 393). A new
// create, or a new read of a group, changes it on purpose — or fails here.
var madeSteps = map[string][]string{
	"ar_create":         {"ar_list", "ar_operation"},
	"backup_create":     {"backup_get", "backup_list"},
	"backup_restore":    {"disk_list"},
	"ca_deploy":         {"ca_list"},
	"cert_import":       {"cert_list", "cert_operation"},
	"cert_issue_le":     {"cert_list", "cert_operation"},
	"deploy":            nil,
	"deploy_apply":      nil,
	"disk_create":       {"disk_list"},
	"dns_record_create": {"dns_operation", "dns_records"},
	"dns_zone_create":   {"dns_operation", "dns_zones"},
	"image_create":      {"image_list"},
	"k8s_add_addon":     {"k8s_operation"},
	"k8s_add_nodepool":  {"k8s_nodepools", "k8s_operation"},
	"k8s_create":        {"k8s_list", "k8s_operation"},
	"kafka_create":      {"kafka_list", "kafka_operation"},
	"lb_create":         {"lb_list", "lb_operation"},
	"pg_backup_create":  {"pg_backups", "pg_operation"},
	"pg_create":         {"pg_list", "pg_operation"},
	"pg_db_create":      {"pg_databases", "pg_operation"},
	"pg_user_create":    {"pg_operation", "pg_users"},
	"provision":         {"vm_list"},
	"redis_create":      {"redis_list", "redis_operation"},
	"s3_bucket_create":  nil,
	"s3_put":            nil,
	"secret_create":     {"secret_list"},
	"sg_rule_create":    {"list_security_groups", "vpc_operation"},
	"snat_create":       {"snat_list"},
	"subnet_create":     {"list_subnets", "vpc_operation"},
	"tg_create":         {"lb_operation", "tg_list"},
	"vpc_create":        {"vpc_list", "vpc_operation"},
	"vpc_route_create":  {"vpc_operation", "vpc_routes"},
}

// The steps a file of made/ runs are a closed list per create, over the
// whole registry: none holds a secret or does something besides
// (k8s_kubeconfig, deploy_plan, vm_console are none of them; A19-42,
// «Риск», 393).
func TestMadeRepeat_StepsAreAClosedListPerCreate(t *testing.T) {
	r := New(nil)
	got := map[string][]string{}
	for name := range allCreated() {
		var steps []string
		for _, st := range r.Tools() {
			if !st.Mutating && r.madeStepReads(name, st) {
				if st.Decl.Sensitive || st.Decl.SideEffects != "" {
					t.Errorf("%s: %s may run from a file of made/ and holds a secret or does something besides", name, st.Name)
				}
				steps = append(steps, st.Name)
			}
		}
		slices.Sort(steps)
		got[name] = steps
	}
	for name, steps := range got {
		if !slices.Equal(steps, madeSteps[name]) {
			t.Errorf("%s: a file of made/ may run %v, the snapshot says %v", name, steps, madeSteps[name])
		}
	}
	for name := range madeSteps {
		if _, ok := got[name]; !ok {
			t.Errorf("%s: in the snapshot, not a create of the registry", name)
		}
	}
}
