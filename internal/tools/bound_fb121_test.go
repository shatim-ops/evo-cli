// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// D31 (audit Fable 1.2.1): a server bound to its projects (--allow-project)
// answers a right call as the unbound one does, and names a broken setup
// for what it is.

// FB121-2: vm_get takes a VM's name in vm_id; bound, the name was checked
// against the ids of the project and the call got NOT_FOUND. A value that
// is not a UUID addresses no VM by id — the handler finds it by name in the
// server's project — so the check passes it; a UUID must still be one of
// the project's (a VM of the project may be named like a foreign VM's id:
// matching names would let that id through).
func TestProjectBound_VMGetByNameInVMID(t *testing.T) {
	f := &ownerFake{}
	r := ownerRegistry(t, f, true, ownProject)
	m, text, isErr := callNamed(t, r, "vm_get", map[string]any{"vm_id": "res-" + resMine[len(resMine)-2:]})
	if isErr && m["reason"] == "NOT_FOUND" && strings.Contains(text, "--allow-project") {
		t.Errorf("vm_get by name in vm_id on a bound server: %s", text)
	}
	m, text, _ = callNamed(t, r, "vm_get", map[string]any{"vm_id": resForeign})
	if m["reason"] != "NOT_FOUND" {
		t.Errorf("vm_get of a foreign VM's id: %s", text)
	}
}

// FB121-5: the factory refusing a resource's project is NOT_FOUND, as for
// a resource that is gone; any other error of the factory (a broken
// profile, no key) is that error, not "not found in the projects of this
// server" — a broken setup hid under "no such resource".
func TestProjectBound_FactoryErrorIsItsOwn(t *testing.T) {
	f := &ownerFake{}
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	dir := t.TempDir()
	r := New(func(over map[string]any) (*engine.Engine, error) {
		p := ownProject
		if v, _ := over["ProjectID"].(string); v != "" {
			p = v
		}
		switch p {
		case ownOther:
			return nil, clierrors.New(clierrors.CodeConfigInvalid, "profile broken-profile: bad YAML", "Fix the profile.")
		case ownForeign:
			return nil, clierrors.Wrap(clierrors.CodeConfigInvalid, "project "+p+" is not allowed on this server", "", ErrProjectNotAllowed)
		}
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: p, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    provision.NewCloudRuAdapter(&http.Client{Transport: f}, ""),
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
		}, nil
	})
	r.SetProjectBound()
	m, text, _ := callNamed(t, r, "pg_get", map[string]any{"cluster_id": resOther})
	if m["reason"] != "CONFIG_INVALID" || !strings.Contains(text, "broken-profile") {
		t.Errorf("a factory error: want it as it is, got %s", text)
	}
	m, text, _ = callNamed(t, r, "pg_get", map[string]any{"cluster_id": resForeign})
	if m["reason"] != "NOT_FOUND" || strings.Contains(text, ownForeign) {
		t.Errorf("a project the server does not allow: want NOT_FOUND naming no project, got %s", text)
	}
}

// The check's own GET refused (403: the key may not read the resource) is
// that refusal, not NOT_FOUND: only a resource the API says is not there
// reads as gone.
func TestProjectBound_CheckGET403IsNotNotFound(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, resForeign) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"access denied"}`))
			return true
		}
		return false
	}}
	r := apiFakeRegistry(t, f)
	r.SetProjectBound()
	m, text, _ := callNamed(t, r, "pg_get", map[string]any{"cluster_id": resForeign})
	if m["reason"] == "NOT_FOUND" || m["reason"] == nil {
		t.Errorf("a 403 of the check: %s", text)
	}
}
