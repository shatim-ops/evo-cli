// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// vm_console is a read (D110, variant (a); brief vm-console.md): where a
// person opens the console of a VM and whether the VM can have one now.
// It writes nothing, names no next step and carries no console field.

// console_available is true only while the VM is running; every other
// state, one evo does not know too, is false with the state quoted. No
// state names a next_tool or next («Риск», conclusion 240, У4), nothing is
// written, and the answer is not an error.
func TestVMConsole_AvailableOnlyWhileRunningAndNoNextStep(t *testing.T) {
	for _, c := range []struct {
		state     string
		available bool
		note      string
	}{
		{"running", true, "a condition of the console, not a promise"},
		{"stopped", false, "vm_start: it is charged, and the person decides"},
		{"starting", false, `state "starting"`},
		{"hibernated", false, `state "hibernated"`},
	} {
		r, w := vmStartWorld(t, c.state)
		m, text, isErr := callNamed(t, r, "vm_console", map[string]any{"name": "web"})
		if isErr {
			t.Fatalf("%s: vm_console is an error: %s", c.state, text)
		}
		if m["console_available"] != c.available || m["state"] != c.state || m["vm_id"] != w.id || m["name"] != "web" {
			t.Errorf("%s: %s", c.state, text)
		}
		if note, _ := m["note"].(string); !strings.Contains(note, c.note) || !strings.Contains(note, "an agent has no use for the page") {
			t.Errorf("%s: note %q does not say %q", c.state, note, c.note)
		}
		if where, _ := m["where"].(string); where != `Open the console of VM "web" in the Cloud.ru web console.` {
			t.Errorf("%s: where = %q", c.state, where)
		}
		for _, k := range []string{"next_tool", "next_args", "next", "url"} {
			if _, has := m[k]; has {
				t.Errorf("%s: the answer has %s: %s", c.state, k, text)
			}
		}
		// No is_serial_ready in the card: no serial_ready, not false.
		if _, has := m["serial_ready"]; has {
			t.Errorf("%s: serial_ready without is_serial_ready in the card: %s", c.state, text)
		}
		if n := w.powerCalls(); n != 0 {
			t.Errorf("%s: written: %d power calls", c.state, n)
		}
	}
}

// A name resolves strictly (strictVMRef): two VMs of the name are a USAGE
// refusal with both ids, a name no VM has is VM_NOT_FOUND (§2, У5).
func TestVMConsole_NameResolvesStrictly(t *testing.T) {
	r, w := vmStartWorld(t, "running")
	w.faultCloud.mu.Lock()
	w.vms = append(w.vms, faultVM{ID: faultID(0x66), Name: "web", Status: "running"})
	w.faultCloud.mu.Unlock()
	m, text, isErr := callNamed(t, r, "vm_console", map[string]any{"name": "web"})
	if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, w.id) || !strings.Contains(text, faultID(0x66)) {
		t.Errorf("vm_console by a name two VMs have: %s", text)
	}
	m, text, isErr = callNamed(t, r, "vm_console", map[string]any{"name": "nope"})
	if !isErr || m["reason"] != clerrors.CodeVMNotFound {
		t.Errorf("vm_console by a name no VM has: %s", text)
	}
}

// The class of a read: not Mutating, no consent, listed on --read-only.
func TestVMConsole_IsARead(t *testing.T) {
	tl, ok := New(nil).Lookup("vm_console")
	if !ok {
		t.Fatal("no vm_console")
	}
	if tl.Mutating || takesConsent(tl) {
		t.Errorf("vm_console: Mutating %v, consent %v; want a read", tl.Mutating, takesConsent(tl))
	}
	if !strictVMRef["vm_console"] {
		t.Error("vm_console is not in strictVMRef")
	}
}

// A card whose console fields carry values: the answer of vm_console has
// none of them, and serial_ready is the card's is_serial_ready. A refusal
// of the card with the same fields: the error has none of them either
// (§5, §5.1).
func TestVMConsole_CardWithConsoleFieldsAnswersNoneOfThem(t *testing.T) {
	const marker = "CONSOLEMARK"
	const id = "11111111-2222-3333-4444-555555555555"
	card := `{"id":"` + id + `","name":"web","state":"running","is_serial_ready":true,` +
		`"vnc_url":"https://c/` + marker + `","vnc_ws":"wss://c/` + marker + `",` +
		`"remote_console_url":"https://c/` + marker + `","remote_console_ws":"wss://c/` + marker + `","remote_console_protocol":"` + marker + `"}`
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(card))
	}))
	defer srv.Close()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		a := provision.NewCloudRuAdapter(srv.Client(), srv.URL)
		a.Backoff = 1
		return &engine.Engine{
			StateDir: t.TempDir(),
			Config:   config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    a,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
	r.SetStateDir(t.TempDir())
	m, text, isErr := callNamed(t, r, "vm_console", map[string]any{"vm_id": id})
	if isErr || m["console_available"] != true || m["serial_ready"] != true {
		t.Fatalf("vm_console on a card: %s", text)
	}
	if strings.Contains(text, marker) {
		t.Errorf("the answer carries a console field: %s", text)
	}
	for _, status = range []int{http.StatusUnprocessableEntity, http.StatusForbidden} {
		_, text, isErr = callNamed(t, r, "vm_console", map[string]any{"vm_id": id})
		if !isErr || strings.Contains(text, marker) {
			t.Errorf("a refusal %d of the card: error %v, %s", status, isErr, text)
		}
	}
}
