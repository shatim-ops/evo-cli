// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// P1 (agent runs of Grok on 1.6.0, G2-1; release plan 1.7 «Присваивать
// только своё», D73 reserved), the deploy of the engine: a VM of the name
// this directory deploys under, which its state does not hold, is someone
// else's. deploy does not adopt it (it does not today) and refuses before
// anything is written — no boot floating IP made and released, no VM
// create that Cloud.ru answers 409 — naming the VM's id, and without
// offering to delete it (block D: deleting is the person's decision).
func TestDeploy_ForeignVMOfTheNameIsRefusedBeforeAWrite(t *testing.T) {
	cloud := &faultCloud{}
	eng, _, dir := faultEngine(t, cloud, &faultDeployer{})
	foreign := "00000000-0000-4000-8000-0000000000f9"
	cloud.mu.Lock()
	cloud.vms = append(cloud.vms, faultVM{ID: foreign, Name: vmNameFor(filepath.Base(dir)), Status: "active", CreatedAt: time.Now().Add(-3 * time.Hour)})
	cloud.mu.Unlock()

	err := faultDeploy(t, eng, dir)
	if err == nil {
		t.Fatal("deploy over someone else's VM of the name succeeded")
	}
	for _, call := range cloud.callLog() {
		if !strings.HasPrefix(call, "List") && !strings.HasPrefix(call, "Get") && call != "PriceCalculation" {
			t.Errorf("deploy wrote to the cloud before refusing someone else's VM of the name: %s", call)
		}
	}
	text := err.Error()
	var ce *clerrors.CLIError
	if errors.As(err, &ce) {
		text += "\n" + ce.Hint
	}
	if ce == nil || ce.Details["existing_id"] != foreign {
		t.Errorf("the refusal does not carry existing_id %s (CLIError.Details, as D49's answers): %v", foreign, err)
	}
	low := strings.ToLower(text)
	for _, w := range []string{"vm delete", "destroy", "удалите", "delete it"} {
		if strings.Contains(low, w) {
			t.Errorf("the refusal offers to delete someone else's VM (%q): %s", w, text)
		}
	}
	if len(cloud.liveVMs()) != 1 {
		t.Errorf("%d VMs after the refusal, want the one that was there", len(cloud.liveVMs()))
	}
}
