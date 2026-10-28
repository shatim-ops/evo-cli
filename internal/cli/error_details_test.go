// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// The answer of a provision cut off by Ctrl-C or the timeout: the VM is
// billed, and the password evo generated is its only copy (FB121-1).
const leftVMAnswer = `{"status":"error","error":"VM vm-1 was created, but provision was interrupted (context canceled)",` +
	`"reason":"VM_PROVISION_FAILED","hint":"check it with vm_get","vm_id":"vm-1","password":"Pw-123"}`

// The --json error of a command carries the fields of the tool's error
// answer beyond the error itself in "details", as the MCP answer carries
// them (parity, cloud-e2e 1.2.1, FB121-1): a script gets the VM left
// billed and its password, an agent got them already. The envelope is the
// same otherwise; with no such fields there is no "details".
func TestHandleError_JSONDetails(t *testing.T) {
	envelope := func(text string) map[string]any {
		t.Helper()
		var out, errOut bytes.Buffer
		handleError(output.NewWithWriters(&out, &errOut, output.ModeJSON, false), toolError(text, callHints("provision")))
		if out.Len() != 0 {
			t.Errorf("stdout: %q", out.String())
		}
		var m struct {
			Error map[string]any `json:"error"`
		}
		if err := json.Unmarshal(errOut.Bytes(), &m); err != nil {
			t.Fatalf("stderr %q: %v", errOut.String(), err)
		}
		return m.Error
	}
	e := envelope(leftVMAnswer)
	if e["reason"] != "VM_PROVISION_FAILED" || !strings.Contains(e["message"].(string), "vm-1 was created") || e["hint"] != "check it with vm_get" {
		t.Errorf("envelope %v", e)
	}
	want := map[string]any{"vm_id": "vm-1", "password": "Pw-123"}
	if !reflect.DeepEqual(e["details"], want) {
		t.Errorf("details %v, want %v", e["details"], want)
	}

	// A refusal of a repeated create names the resource there is.
	e = envelope(`{"status":"error","error":"a cluster named k already exists","reason":"USAGE","existing_id":"c-1"}`)
	if !reflect.DeepEqual(e["details"], map[string]any{"existing_id": "c-1"}) {
		t.Errorf("existing_id: %v", e)
	}
	// An error without fields of its own: the envelope as before.
	e = envelope(`{"status":"error","error":"disk d not found","reason":"NOT_FOUND"}`)
	if _, ok := e["details"]; ok {
		t.Errorf("details without fields: %v", e)
	}
}

// Without --json the password evo generated is in the text of the error
// too, in the words of evo vm create: after Ctrl-C or the timeout it is
// the only way in to the VM.
func TestHandleError_TextPassword(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	var out, errOut bytes.Buffer
	handleError(output.NewWithWriters(&out, &errOut, output.ModePlain, false), toolError(leftVMAnswer, callHints("provision")))
	if !strings.Contains(errOut.String(), vmPasswordLine("Pw-123")) {
		t.Errorf("stderr:\n%s", errOut.String())
	}
	if strings.Contains(errOut.String(), "vm_id") {
		t.Errorf("raw field in the text:\n%s", errOut.String())
	}
}
