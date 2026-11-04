// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// D76 over MCP: logs of a deployment deploy has not finished answers
// DEPLOY_IN_PROGRESS with the record's status and deploy as the next step,
// and does not go to the VM (the cloud of this world answers nothing:
// a read of it would panic).
func TestLogs_AnUnfinishedDeployAnswersInProgress(t *testing.T) {
	dir := t.TempDir()
	writeRecords(t, dir, state.DeploymentRecord{ID: "0000a1a1-0000-4000-8000-0000000000c1", VMID: "0000a1a1-0000-4000-8000-0000000000c1",
		VMIP: "203.0.113.9", SSHUser: "user1", Status: "creating"})
	r := qaBRegistry(t, nil, dir)
	text, isErr := r.handlers["logs"](context.Background(), map[string]any{})
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || !isErr {
		t.Fatalf("logs: want an error answer: %s", text)
	}
	if m["reason"] != "DEPLOY_IN_PROGRESS" || m["deployment_status"] != "creating" || m["next_tool"] != "deploy" {
		t.Errorf("logs of a creating deployment: %s, want DEPLOY_IN_PROGRESS with deployment_status creating and next_tool deploy", text)
	}
}
