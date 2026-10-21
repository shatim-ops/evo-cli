// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Fictitious PostgreSQL clusters of the --allow-project tests: one in the
// allowed project, one in another.
const (
	clusterAllowed = "00000000-0000-4000-8000-0000000000c1"
	clusterOther   = "00000000-0000-4000-8000-0000000000c2"
)

// pgTrip is the Managed PostgreSQL API of the test: a cluster by id names
// its project (project_id, openapi-postgresql.yaml), a delete answers an
// operation. Any other Cloud.ru host is a 500; the rest goes to the
// transport installed before (setupCLI's fake IAM).
type pgTrip struct {
	base http.RoundTripper
	mu   sync.Mutex
	reqs []string
}

func (p *pgTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(r.URL.Hostname(), ".cloud.ru") {
		return p.base.RoundTrip(r)
	}
	p.mu.Lock()
	p.reqs = append(p.reqs, r.Method+" "+r.URL.Path)
	p.mu.Unlock()
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	project := map[string]string{clusterAllowed: allowedProject, clusterOther: otherProject}
	id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	switch {
	case r.URL.Host != "postgresql.api.cloud.ru":
		http.Error(rec, "unexpected "+r.URL.Host, http.StatusInternalServerError)
	case r.Method == http.MethodDelete:
		_ = json.NewEncoder(rec).Encode(map[string]any{"id": "op-delete", "done": false})
	case strings.HasPrefix(r.URL.Path, "/v1/operations/"):
		_ = json.NewEncoder(rec).Encode(map[string]any{"id": id, "done": true})
	case project[id] != "":
		_ = json.NewEncoder(rec).Encode(map[string]any{"id": id, "name": "pg-" + id[len(id)-2:], "project_id": project[id]})
	default:
		http.Error(rec, `{"message":"not found"}`, http.StatusNotFound)
	}
	return rec.Result(), nil
}

func (p *pgTrip) take() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.reqs
	p.reqs = nil
	return out
}

func installPGTrip(t *testing.T) *pgTrip {
	t.Helper()
	trip := &pgTrip{base: http.DefaultTransport}
	prev := http.DefaultTransport
	http.DefaultTransport = trip
	t.Cleanup(func() { http.DefaultTransport = prev })
	return trip
}

// REL12-3: with --allow-project a tool that names a resource by id acted
// on the resource of any project the key sees — pg_get read and pg_delete
// deleted a cluster of another project, their requests carry no project.
// Now the cluster's project is checked first, over stdio and HTTP: another
// project is the H9 refusal (CONFIG_INVALID, the hint of
// checkAllowedProject), and the delete is not sent; a cluster of the
// allowed project works as before.
func TestAllowProject_ResourceByIDOfAnotherProject(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", allowedProject)
	trip := installPGTrip(t)
	allowed := []string{allowedProject}
	var h9 *clerrors.CLIError
	if !stderrors.As(checkAllowedProject(allowed, otherProject), &h9) {
		t.Fatal("checkAllowedProject: no CLIError")
	}
	for transport, call := range mcpTransports(t, allowed) {
		for _, c := range []struct {
			tool string
			args map[string]any
		}{
			{"pg_get", map[string]any{"id": clusterOther}},
			{"pg_delete", map[string]any{"id": clusterOther, "confirmed": true}},
			{"pg_databases", map[string]any{"cluster_id": clusterOther}},
		} {
			trip.take()
			a := call(c.tool, c.args)
			obj := parseObject(a.text)
			msg, _ := obj["error"].(string)
			if !a.isErr || obj["reason"] != h9.Code || obj["hint"] != h9.Hint ||
				!strings.Contains(msg, clusterOther) || !strings.Contains(msg, h9.Message) {
				t.Errorf("%s %s: %s, want %s %q with the hint of H9", transport, c.tool, a.text, h9.Code, h9.Message)
			}
			if strings.Contains(a.text, "pg-c2") {
				t.Errorf("%s %s: the answer shows the cluster: %s", transport, c.tool, a.text)
			}
			if got := trip.take(); len(got) != 1 || got[0] != "GET /v1/clusters/"+clusterOther {
				t.Errorf("%s %s: requests %v, want only the check's GET", transport, c.tool, got)
			}
		}
		// An operation this server did not start.
		a := call("pg_operation", map[string]any{"operation_id": "op-elsewhere"})
		if obj := parseObject(a.text); !a.isErr || obj["reason"] != clerrors.CodeConfigInvalid {
			t.Errorf("%s pg_operation: %s, want CONFIG_INVALID", transport, a.text)
		}
		if got := trip.take(); len(got) != 0 {
			t.Errorf("%s pg_operation: requests %v, want none", transport, got)
		}

		a = call("pg_get", map[string]any{"id": clusterAllowed})
		if a.isErr || parseObject(a.text)["project_id"] != allowedProject {
			t.Errorf("%s pg_get of the allowed project: %s", transport, a.text)
		}
		a = call("pg_delete", map[string]any{"id": clusterAllowed, "confirmed": true})
		if a.isErr || parseObject(a.text)["operation_id"] != "op-delete" {
			t.Errorf("%s pg_delete of the allowed project: %s", transport, a.text)
		}
		// Started by this server: polled.
		if a = call("pg_operation", map[string]any{"operation_id": "op-delete"}); a.isErr {
			t.Errorf("%s pg_operation of its own delete: %s", transport, a.text)
		}
		trip.take()
	}
}

// Without --allow-project, MCP and the CLI ('evo tools call', the command
// trees) act on a cluster of any project as before, with no extra request.
func TestAllowProject_ResourceByIDNotBound(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", allowedProject)
	trip := installPGTrip(t)
	for transport, call := range mcpTransports(t, nil) {
		trip.take()
		a := call("pg_get", map[string]any{"id": clusterOther})
		if a.isErr || parseObject(a.text)["project_id"] != otherProject {
			t.Errorf("%s pg_get: %s", transport, a.text)
		}
		if got := trip.take(); len(got) != 1 {
			t.Errorf("%s pg_get: requests %v, want one", transport, got)
		}
		if a := call("pg_operation", map[string]any{"operation_id": "op-elsewhere"}); a.isErr {
			t.Errorf("%s pg_operation: %s", transport, a.text)
		}
	}
	reg := newRegistry()
	tl, _ := reg.Lookup("pg_delete")
	trip.take()
	text, isErr := tl.Handler(context.Background(), map[string]any{"id": clusterOther, "confirmed": true})
	if isErr {
		t.Fatalf("CLI pg_delete: %s", text)
	}
	if got := trip.take(); len(got) != 1 || got[0] != "DELETE /v1/clusters/"+clusterOther {
		t.Errorf("CLI pg_delete: requests %v, want the DELETE alone", got)
	}
}
