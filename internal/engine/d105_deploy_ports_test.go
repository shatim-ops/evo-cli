// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"
)

// D105 is a rule of the tools that change the rules of an existing group
// (open_ports, sg_rule_create). deploy opens the ports of its compose file
// itself, as part of the consent to the deploy — from anywhere, in the
// zone's group — and is not changed (owner's decision 12.11, variant 1):
// it adds its rules as before and reads no list of interfaces (the world
// counts the reads).
func TestD105_DeployOpensItsPortsAsBefore(t *testing.T) {
	cloud := &mockCloudAPI{}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	opts := DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: "small", Region: "az-1", NonInteractive: true, Yes: true}
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	if cloud.interfaceReads != 0 {
		t.Errorf("deploy read the interfaces of the project %d time(s): the rule of D105 is not deploy's", cloud.interfaceReads)
	}
	if len(cloud.addedRules) == 0 {
		t.Fatal("deploy added no rule for the ports of its compose file")
	}
	for _, r := range cloud.addedRules {
		if r.Direction != "ingress" || r.RemoteIPPrefix != "0.0.0.0/0" {
			t.Errorf("deploy's rule %+v, want ingress from anywhere as before", r)
		}
	}
}
