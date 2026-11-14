// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// P6 as a class (release plan 1.7, B; agent runs of Grok on 1.6.0): every
// way evo deletes a VM closes the port rules it opened for that VM in
// another folder — found by the label with the VM's id, not by this
// folder's state — and only those. destroy is the one tool that deletes a
// VM (Decl.Delete); it names its VM by vm_id or by name, and
// stack_destroy deletes a stack's VM through it. The CLI's vm delete is the
// same handler (TestParity_*). Each way runs from folder B, whose state
// records nothing, in a project where folder A's evo opened port 8080 for
// the VM next to another VM's rule and a person's.
func TestRuleLabel_EveryVMDeleteClosesItsRulesFromAnotherFolder(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range []struct {
		name string
		call func(t *testing.T, r *Registry) (string, bool)
	}{
		{"destroy by vm_id", func(_ *testing.T, r *Registry) (string, bool) {
			return r.handlers["destroy"](context.Background(), map[string]any{"vm_id": labelVMID, "confirmed": true})
		}},
		{"destroy by name", func(_ *testing.T, r *Registry) (string, bool) {
			return r.handlers["destroy"](context.Background(), map[string]any{"name": "web", "confirmed": true})
		}},
		{"stack_destroy of a stack's VM", func(t *testing.T, r *Registry) (string, bool) {
			stackHome(t)
			j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
			loaded, err := stack.LoadJob(j.ID)
			if err != nil {
				t.Fatal(err)
			}
			loaded.Status = "done"
			for _, st := range loaded.Steps {
				st.Status, st.ResourceID = "done", labelVMID
				loaded.Outputs[st.Key] = labelVMID
			}
			if err := loaded.Save(); err != nil {
				t.Fatal(err)
			}
			return r.handlers["stack_destroy"](context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
		}},
	} {
		covered[strings.Fields(tc.name)[0]] = true
		t.Run(tc.name, func(t *testing.T) {
			cloud := newLabelCloud(true)
			text, isErr := tc.call(t, folderB(t, cloud))
			if isErr {
				t.Fatalf("the VM's delete failed: %s", text)
			}
			if !cloud.vmGone {
				t.Fatalf("the VM was not deleted: %s", text)
			}
			if strings.Join(cloud.deleted, ",") != "rule-a" {
				t.Errorf("deleted rules %v, want rule-a alone (the VM's, opened from another folder): %s", cloud.deleted, text)
			}
		})
	}
	// A new tool that deletes a VM joins the cases above, or this fails.
	for name, d := range allDeletes() {
		if d.what == "VM" && !covered[name] {
			t.Errorf("%s deletes a VM, but no case here deletes one with it from another folder", name)
		}
	}
}
