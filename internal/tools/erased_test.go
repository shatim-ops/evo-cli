// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// erasedDeletes are the deletes of a DBaaS cluster, whose service keeps
// a deleted cluster in its answers as ERASED (status ERASED, health
// HEALTHY) and refuses its delete — 500, or 400 FailedPrecondition (live
// run 27.10: a repeated pg_delete and kafka_delete).
var erasedDeletes = []string{"kafka_delete", "pg_delete", "redis_delete"}

// erasedWorld has the cluster liveID, ERASED; it refuses every write the
// way the service does, and counts them.
func erasedWorld(writes *atomic.Int32) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writes.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"code":9,"message":"FailedPrecondition: cluster is erased"}`)
			return true
		}
		_, _ = io.WriteString(w, `{"id":"`+liveID+`","name":"`+liveName+`","status":"ERASED","health":"HEALTHY",`+
			`"project_id":"`+dbaasProject+`","projectId":"`+dbaasProject+`"}`)
		return true
	}
}

// A cluster the service shows ERASED is deleted: its delete is a repeated
// one and answers already_deleted, exit 0, existed false — in the preview,
// with no question and no write, and after consent (D26; CLAUDE.md §6,
// axis 3: "already gone" is not an error, and an error is not "wait until
// READY" for a cluster that never will be). evo reads ERASED as gone
// already: the name of an ERASED cluster is free (takenName), a stack
// takes it as deleted.
func TestAlreadyDeleted_ErasedClusterIsDeleted(t *testing.T) {
	for _, name := range erasedDeletes {
		d, ok := deleteTargets[name]
		if !ok {
			t.Fatalf("%s is not in deleteTargets", name)
		}
		for _, confirmed := range []bool{false, true} {
			var writes atomic.Int32
			r := erasedRegistry(t, erasedWorld(&writes))
			args := map[string]any{d.arg: liveID}
			if confirmed {
				args["confirmed"] = true
			}
			m, text, isErr := callNamed(t, r, name, args)
			if isErr || m["status"] != "already_deleted" || m["existed"] != false || m[d.arg] != liveID {
				t.Errorf("%s confirmed=%v on an ERASED cluster: want already_deleted, exit 0: %s", name, confirmed, text)
			}
			if n := writes.Load(); n != 0 {
				t.Errorf("%s confirmed=%v: %d writes to an ERASED cluster, want none", name, confirmed, n)
			}
		}
	}
}

// erasedRegistry is goneRegistry on the world own.
func erasedRegistry(t *testing.T, own func(http.ResponseWriter, *http.Request) bool) *Registry {
	t.Helper()
	stackHome(t)
	f := &apiFake{own: own}
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    provision.NewCloudRuAdapter(&http.Client{Transport: f}, ""),
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
		}, nil
	})
}
