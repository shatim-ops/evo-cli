// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// consoleFields are the fields of the card of a VM (and of an item of the
// list of VMs, the answer of PUT and of rebuild) that hold a link to its
// console: access to the VM, a secret (D110; «Риск», conclusion 204, У1).
var consoleFields = []string{"vnc_url", "vnc_ws", "remote_console_url", "remote_console_ws"}

// The link to the console of a VM is in the card, in the list of VMs and in
// the answer of PUT (vm_resize): every type evo reads a VM into drops it, so
// nothing evo answers, prints or keeps can hold it (D110; «Риск»,
// conclusion 204, У1). A field added to such a type that keeps the link —
// by its name, or a map or raw JSON of the whole card — fails here.
func TestConsoleFieldsAreNotRead(t *testing.T) {
	card := map[string]any{"id": "vm-1", "name": "web", "state": "running",
		"interfaces": []any{map[string]any{"id": "if-1", "ip_address": "10.0.0.8", "primary": true, "state": "created"}}}
	for i, f := range consoleFields {
		card[f] = fmt.Sprintf("https://console.example/%d-MARKER", i)
	}
	body, _ := json.Marshal(card)
	list, _ := json.Marshal(map[string]any{"items": []any{card}, "total": 1})
	for name, target := range map[string]any{
		"VMDetail (GET /vms/{id}, PUT /vms/{id})": &VMDetail{},
		"VM (an item of GET /vms)":                &VM{},
	} {
		if err := json.Unmarshal(body, target); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out, _ := json.Marshal(target)
		if strings.Contains(string(out), "MARKER") {
			t.Errorf("%s keeps a link to the console: %s", name, out)
		}
	}
	var page struct {
		Items []VM `json:"items"`
	}
	if err := json.Unmarshal(list, &page); err != nil {
		t.Fatal(err)
	}
	if out, _ := json.Marshal(page); strings.Contains(string(out), "MARKER") {
		t.Errorf("the list of VMs keeps a link to the console: %s", out)
	}
}
