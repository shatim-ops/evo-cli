// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// deploy opens the ports of its compose file itself. A port a rule lets in
// only from another security group gets no rule (as before) — and deploy
// does not say it is open: the address would not answer on it from outside
// that group (owner's decision 12.11, variant «б»; «Риск», conclusion 169).
func TestShowOpenedPorts_APortOnlyAGroupLetsInIsNotCalledOpen(t *testing.T) {
	var buf bytes.Buffer
	p := output.NewWithWriter(&buf, output.ModeText, false)
	res := sgrules.OpenResult{AlreadyOpen: []string{"80"}, OnlyFromGroups: []sgrules.GroupOnlyPort{{Port: "8080", Groups: []string{"db"}}}}
	ShowOpenedPorts(p, res, &state.DeploymentRecord{}, "SSH-access_ru.AZ-1")
	text := buf.String()
	if !strings.Contains(text, "8080 is NOT open") || !strings.Contains(text, `"db"`) || !strings.Contains(text, "sg-rule-create") {
		t.Errorf("deploy does not say 8080 is let in only from the group: %s", text)
	}
	if strings.Contains(text, "8080 is already open") || strings.Contains(text, "80,8080") {
		t.Errorf("deploy calls 8080 open: %s", text)
	}
}

func TestOpenPorts_KeepsWhatOnlyGroupsLetInForTheAnswer(t *testing.T) {
	cloud := &mockCloudAPI{listSecurityGroupsFn: func(context.Context, string) ([]*provision.SecurityGroup, error) {
		return []*provision.SecurityGroup{{ID: "sg-1", Name: "SSH-access_ru.AZ-1", Rules: []provision.SGRule{
			{ID: "r-group", Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", State: "created",
				RemoteSecurityGroup: &provision.SGRuleGroup{ID: "sg-db", Name: "db"}}}}}, nil
	}}
	eng, _ := newTestEngine(t, cloud, newMockDeployer())
	rec := &state.DeploymentRecord{VMID: "vm-1", SecurityGroupName: "SSH-access_ru.AZ-1"}
	eng.openPorts(context.Background(), "p", "sg-1", []int{8080}, nil, "", rec)
	if len(eng.portsOnlyFromGroups) != 1 || eng.portsOnlyFromGroups[0].Port != "8080" {
		t.Errorf("portsOnlyFromGroups = %+v, want 8080 for the answer of deploy", eng.portsOnlyFromGroups)
	}
	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	if len(cloud.addedRules) != 0 {
		t.Errorf("deploy added %d rule(s) for a port a group lets in: the access is not changed until the major", len(cloud.addedRules))
	}
}
