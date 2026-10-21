// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Fictitious projects and resources of the --allow-project tests (REL12-3):
// the server works in ownProject; ownOther is allowed too; ownForeign is
// not. Every service of the fake holds one resource of each, with the
// same id in every service.
const (
	ownProject = "00000000-0000-4000-8000-0000000000aa"
	ownOther   = "00000000-0000-4000-8000-0000000000ac"
	ownForeign = "00000000-0000-4000-8000-0000000000bb"
	resMine    = "00000000-0000-4000-8000-00000000f0a1"
	resOther   = "00000000-0000-4000-8000-00000000f0c1"
	resForeign = "00000000-0000-4000-8000-00000000f0b1"
)

// idLike reports an argument that names a resource by id: *_id, *_ids,
// id, and the arguments that take a resource's id or name.
func idLike(arg string) bool {
	switch arg {
	case "id", "vm", "fip", "image", "security_group", "targets", "attach_to":
		return true
	}
	return strings.HasSuffix(arg, "_id") || strings.HasSuffix(arg, "_ids")
}

// Every argument that names a resource by id is declared in idArgs: with
// the kind a project-bound registry checks, or with the reason it needs
// none. A new tool (or argument) without a declaration fails here, so the
// check of --allow-project cannot be forgotten (REL12-3).
func TestProjectBound_IDArgsDeclared(t *testing.T) {
	r := New(nil)
	for _, tl := range r.Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		declared := map[string]bool{}
		for _, a := range idArgs[tl.Name] {
			if declared[a.name] {
				t.Errorf("%s: %s declared twice", tl.Name, a.name)
			}
			declared[a.name] = true
			if _, ok := props[a.name]; !ok {
				t.Errorf("%s: declared argument %s is not in the schema", tl.Name, a.name)
			}
			if (a.kind == nil) == (a.why == "") {
				t.Errorf("%s.%s: needs exactly one of a kind to check or a reason", tl.Name, a.name)
			}
			if a.kind != nil && a.kind != kindOperation {
				if _, ok := r.Lookup(a.kind.listTool); !ok {
					t.Errorf("%s.%s: the list tool %q of %s is not a tool", tl.Name, a.name, a.kind.listTool, a.kind.what)
				}
			}
		}
		for p := range props {
			if idLike(p) && !declared[p] {
				t.Errorf("%s takes %s, which names a resource: declare it in idArgs (a kind to check, or why none is needed)", tl.Name, p)
			}
		}
		if strings.HasSuffix(tl.Name, "_operation") {
			ok := false
			for _, a := range idArgs[tl.Name] {
				ok = ok || a.name == "operation_id" && a.kind == kindOperation
			}
			if !ok {
				t.Errorf("%s: operation_id must be checked as an operation", tl.Name)
			}
		}
	}
	for name := range idArgs {
		if _, ok := r.Lookup(name); !ok {
			t.Errorf("idArgs names %s, which is not a tool", name)
		}
	}
}

// ownerFake is every Cloud.ru API the checks and the handlers read: a
// resource by id names its project (both spellings), a list of a project
// holds its resource, any other method answers an operation. It records
// "METHOD host/path" of every request.
type ownerFake struct {
	mu   sync.Mutex
	reqs []string
}

func (f *ownerFake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.Host+r.URL.Path)
	f.mu.Unlock()
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	f.serve(rec, r)
	return rec.Result(), nil
}

func (f *ownerFake) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

// projectOf is the project of a resource of the fake.
var projectOf = map[string]string{resMine: ownProject, resOther: ownOther, resForeign: ownForeign}

// zoneOf is the DNS zone of a record of the fake: the zone of the same
// project, another id than the record's.
var zoneOf = map[string]string{resMine: zoneMine, resOther: zoneOther, resForeign: zoneForeign}

const (
	zoneMine    = "00000000-0000-4000-8000-00000000e0a1"
	zoneOther   = "00000000-0000-4000-8000-00000000e0c1"
	zoneForeign = "00000000-0000-4000-8000-00000000e0b1"
)

// resourceOf is the resource of a project of the fake.
var resourceOf = map[string]string{ownProject: resMine, ownOther: resOther, ownForeign: resForeign}

func ownerObject(id string) map[string]any {
	p := projectOf[id]
	return map[string]any{
		"id": id, "name": "res-" + id[len(id)-2:], "status": "READY", "state": "available",
		"project_id": p, "projectId": p, "type": provision.ImageTypePrivate,
		"publicZoneId": zoneOf[id], "privateZoneId": zoneOf[id],
	}
}

func (f *ownerFake) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "op-new", "done": false, "resource_id": resMine})
		return
	}
	if strings.Contains(r.URL.Path, "operation") {
		seg := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		_ = json.NewEncoder(w).Encode(map[string]any{"id": seg, "done": true})
		return
	}
	for id := range projectOf {
		if strings.Contains(r.URL.Path, id) {
			_ = json.NewEncoder(w).Encode(ownerObject(id))
			return
		}
	}
	// A list: the resource of the project the query names.
	q := r.URL.Query()
	p := q.Get("project_id")
	if p == "" {
		p = q.Get("projectId")
	}
	items, zones := []any{}, []any{}
	if id := resourceOf[p]; id != "" {
		items = append(items, ownerObject(id))
		zones = append(zones, ownerObject(id), map[string]any{"id": zoneOf[id], "name": "zone"})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items, "total": len(items), "clusters": items, "zones": zones, "vpcs": items,
		"balancers": items, "target_groups": items, "certificates": items, "records": items,
	})
}

// ownerStateDirs is the state directory of each test: the registries of
// one test share it, as the preview of deploy names its VM after it.
var ownerStateDirs sync.Map

// ownerRegistry is a registry on the fake whose factory admits the
// projects in allowed, as evo mcp --allow-project's does
// (checkAllowedProject); bound calls SetProjectBound.
func ownerRegistry(t *testing.T, f *ownerFake, bound bool, allowed ...string) *Registry {
	t.Helper()
	dir, loaded := ownerStateDirs.LoadOrStore(t, t.TempDir())
	if !loaded {
		t.Cleanup(func() { ownerStateDirs.Delete(t) })
	}
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	r := New(func(over map[string]any) (*engine.Engine, error) {
		p := ownProject
		if v, _ := over["ProjectID"].(string); v != "" {
			p = v
		}
		if !slices.Contains(allowed, p) {
			return nil, clierrors.New(clierrors.CodeConfigInvalid, "project "+p+" is not allowed on this server",
				"The server runs with --allow-project; ask its operator to add the project.")
		}
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: p, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    provision.NewCloudRuAdapter(&http.Client{Transport: f}, ""),
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir.(string),
		}, nil
	})
	if bound {
		r.SetProjectBound()
	}
	return r
}

// ownerCase is one checked argument of a tool, with the value that names
// the resource of project p.
type ownerCase struct {
	tool string
	arg  idArg
}

func (c ownerCase) args(id string) map[string]any {
	if c.arg.name == "targets" {
		return map[string]any{"targets": []any{map[string]any{"id": id}}}
	}
	return map[string]any{c.arg.name: id}
}

func ownerCases(t *testing.T) []ownerCase {
	t.Helper()
	var out []ownerCase
	for tool := range idArgs {
		for _, a := range checkedIDArgs(tool) {
			out = append(out, ownerCase{tool, a})
		}
	}
	slices.SortFunc(out, func(a, b ownerCase) int { return strings.Compare(a.tool+"."+a.arg.name, b.tool+"."+b.arg.name) })
	if len(out) < 60 {
		t.Fatalf("only %d checked arguments", len(out))
	}
	return out
}

// The id of an operation another server (or the console) started, and of
// one this server started.
const (
	opForeign = "op-foreign"
	opMine    = "op-mine"
)

// callOwned runs a tool of r with args, confirmed when it takes consent.
func callOwned(t *testing.T, r *Registry, name string, args map[string]any, confirmed bool) (map[string]any, string, bool) {
	t.Helper()
	tl, _ := r.Lookup(name)
	if confirmed && takesConsent(tl) {
		args["confirmed"] = true
	}
	return callNamed(t, r, name, args)
}

// A resource of a project outside --allow-project, named by id: every
// tool refuses with the H9 refusal (CONFIG_INVALID) before its handler —
// the confirmed delete, change or create sends nothing, the read returns
// nothing of the resource; only the check's own reads reach the cloud.
func TestProjectBound_ForeignResourceRefused(t *testing.T) {
	stackHome(t)
	for _, c := range ownerCases(t) {
		f := &ownerFake{}
		r := ownerRegistry(t, f, true, ownProject, ownOther)
		id := resForeign
		if c.arg.kind == kindOperation {
			id = opForeign
		}
		m, text, isErr := callOwned(t, r, c.tool, c.args(id), true)
		if !isErr || m["reason"] != clierrors.CodeConfigInvalid || !strings.Contains(text, id) {
			t.Errorf("%s %s=%s: %s, want CONFIG_INVALID naming %s", c.tool, c.arg.name, id, text, id)
		}
		if strings.Contains(text, "res-b1") || strings.Contains(text, zoneForeign) {
			t.Errorf("%s: the answer shows the foreign resource: %s", c.tool, text)
		}
		for _, q := range f.log() {
			if !strings.HasPrefix(q, "GET ") {
				t.Errorf("%s %s=%s sent %s", c.tool, c.arg.name, id, q)
			}
		}
	}
}

// A resource of an allowed project works as before: the bound call sends
// the check's reads (GET only), then exactly what the call sends without
// --allow-project, and answers the same; without --allow-project no check
// runs. Calls go without confirmed (previews), so nothing heavy runs; the
// tools that run kubectl or helm are left out.
func TestProjectBound_AllowedResourceAsBefore(t *testing.T) {
	stackHome(t)
	execTools := map[string]bool{"k8s_kubectl": true, "k8s_apply": true, "k8s_helm": true}
	for _, c := range ownerCases(t) {
		if execTools[c.tool] {
			continue
		}
		ids := []string{resMine}
		if c.arg.kind == kindOperation {
			ids = []string{opMine}
		} else if isFieldKind(c.arg.kind) {
			// The answer names the project: another allowed project is
			// allowed too.
			ids = append(ids, resOther)
		}
		for _, id := range ids {
			plain, bound := &ownerFake{}, &ownerFake{}
			rp := ownerRegistry(t, plain, false, ownProject, ownOther)
			_, want, _ := callOwned(t, rp, c.tool, c.args(id), false)
			rb := ownerRegistry(t, bound, true, ownProject, ownOther)
			rb.ops.remember(groupOf(c.tool), opMine)
			_, got, _ := callOwned(t, rb, c.tool, c.args(id), false)
			if got != want {
				t.Errorf("%s %s=%s bound answers\n%s\nwant (not bound)\n%s", c.tool, c.arg.name, id, got, want)
			}
			pl, bl := plain.log(), bound.log()
			if len(bl) < len(pl) || !slices.Equal(bl[len(bl)-len(pl):], pl) {
				t.Errorf("%s %s=%s: bound requests %v do not end with the unbound ones %v", c.tool, c.arg.name, id, bl, pl)
				continue
			}
			for _, q := range bl[:len(bl)-len(pl)] {
				if !strings.HasPrefix(q, "GET ") {
					t.Errorf("%s: the check sent %s", c.tool, q)
				}
			}
			if c.arg.kind != kindOperation && len(bl) == len(pl) {
				t.Errorf("%s %s: the bound call made no check: %v", c.tool, c.arg.name, bl)
			}
		}
	}
}

// isFieldKind reports a kind whose answer names its project (not a list).
func isFieldKind(k *ownerKind) bool {
	for _, f := range []*ownerKind{kindPG, kindNLB, kindTG, kindVPC, kindK8s, kindCert, kindImage} {
		if k == f {
			return true
		}
	}
	return false
}

// In an allowed project the confirmed delete reaches the cloud as before.
func TestProjectBound_AllowedDeleteSent(t *testing.T) {
	for _, c := range []struct{ tool, arg, want string }{
		{"pg_delete", "id", "DELETE postgresql.api.cloud.ru/v1/clusters/" + resMine},
		{"kafka_delete", "cluster_id", "DELETE kafka.api.cloud.ru/v1/clusters/" + resMine},
		{"vpc_delete", "id", "DELETE vpc.api.cloud.ru/v1/vpcs/" + resMine},
		{"disk_delete", "disk_id", "DELETE compute.api.cloud.ru/api/v1/disks/" + resMine},
		{"cert_delete", "id", "DELETE certificatemanager.api.cloud.ru/v1/certificate/" + resMine},
	} {
		f := &ownerFake{}
		r := ownerRegistry(t, f, true, ownProject)
		_, text, isErr := callOwned(t, r, c.tool, map[string]any{c.arg: resMine}, true)
		if isErr || !slices.Contains(f.log(), c.want) {
			t.Errorf("%s: %s (error %v), requests %v, want %s", c.tool, text, isErr, f.log(), c.want)
		}
	}
}

// Without --allow-project a resource of any project is read and deleted
// as before, with no extra request.
func TestProjectBound_NotBoundNoCheck(t *testing.T) {
	f := &ownerFake{}
	r := ownerRegistry(t, f, false, ownProject, ownForeign)
	_, text, isErr := callOwned(t, r, "pg_get", map[string]any{"id": resForeign}, false)
	if isErr || !strings.Contains(text, ownForeign) {
		t.Fatalf("pg_get: %s", text)
	}
	_, text, isErr = callOwned(t, r, "pg_operation", map[string]any{"operation_id": opForeign}, false)
	if isErr {
		t.Fatalf("pg_operation: %s", text)
	}
	if want := []string{"GET postgresql.api.cloud.ru/v1/clusters/" + resForeign, "GET postgresql.api.cloud.ru/v1/operations/" + opForeign}; !slices.Equal(f.log(), want) {
		t.Errorf("requests %v, want %v", f.log(), want)
	}
}

// An operation this server started is polled; one of another group or
// another server is refused, before any request.
func TestProjectBound_OperationsOfThisServer(t *testing.T) {
	f := &ownerFake{}
	r := ownerRegistry(t, f, true, ownProject)
	m, text, isErr := callOwned(t, r, "vpc_create", map[string]any{"name": "net"}, true)
	if isErr || m["operation_id"] != "op-new" {
		t.Fatalf("vpc_create: %s", text)
	}
	if _, text, isErr := callOwned(t, r, "vpc_operation", map[string]any{"operation_id": "op-new"}, false); isErr {
		t.Errorf("vpc_operation of this server: %s", text)
	}
	if !slices.Contains(f.log(), "GET vpc.api.cloud.ru/v1/vpcs/operations/op-new") {
		t.Errorf("the operation was not read: %v", f.log())
	}
	n := len(f.log())
	for _, c := range []struct{ tool, id string }{{"vpc_operation", opForeign}, {"pg_operation", "op-new"}} {
		m, text, isErr := callOwned(t, r, c.tool, map[string]any{"operation_id": c.id}, false)
		if !isErr || m["reason"] != clierrors.CodeConfigInvalid {
			t.Errorf("%s %s: %s, want CONFIG_INVALID", c.tool, c.id, text)
		}
	}
	if got := f.log()[n:]; len(got) != 0 {
		t.Errorf("refused operations sent %v", got)
	}
	// cert_issue_le names its operation as operation.id.
	if got := answerOperationID(`{"status":"issuing","operation":{"id":"op-cert"}}`); got != "op-cert" {
		t.Errorf("operation of cert_issue_le: %q", got)
	}
}

// A running stack job of the server's project polls its operations on a
// bound server, also after a restart: crankJob vouches for them.
func TestProjectBound_StackJobPollsItsOperations(t *testing.T) {
	stackHome(t)
	j := planJob(t, `{"name":"g","vpc":{"name":"net"}}`)
	j.ProjectID = ownProject
	j.Status = "running"
	for _, st := range j.Steps {
		if st.Key == "vpc" {
			st.Status, st.OpID = "waiting", "op-stack"
		}
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	f := &ownerFake{}
	r := ownerRegistry(t, f, true, ownProject)
	if _, err := r.crankJob(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.log(), "GET vpc.api.cloud.ru/v1/vpcs/operations/op-stack") {
		t.Errorf("the job's operation was not polled: %v", f.log())
	}
}

// A resource whose answer names no project is refused: unknown is not
// allowed.
func TestProjectBound_NoProjectInAnswerRefused(t *testing.T) {
	f := &ownerFake{}
	r := ownerRegistry(t, f, true, ownProject)
	c := &ownerCall{r: r, args: map[string]any{}}
	err := c.admit(kindPG, resMine, "")
	if err == nil || !strings.Contains(err.Error(), "names no project") {
		t.Errorf("admit without a project: %v", err)
	}
	var ce *clierrors.CLIError
	if !stderrors.As(err, &ce) || ce.Code != clierrors.CodeConfigInvalid {
		t.Errorf("admit without a project: %v, want CONFIG_INVALID", err)
	}
}
