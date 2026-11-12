// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The world of the finding (release plan 1.9; found at #1490): a rule
// whose source is another security group names no prefix, and Coverage
// reads «no prefix» as «from anywhere» (D52). open_ports and deploy then
// take the port for open to the source asked for and add no rule of their
// own — and the service is out of reach of everyone but the interfaces of
// that group. Red until the decision: the fix changes the answers of
// shipped commands («already open», exit 0 → a question, exit 3).
func TestCoverage_ARuleFromAGroupDoesNotOpenThePortToAnAddress(t *testing.T) {
	rules := []*provision.SGRule{{ID: "r-1", Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "80:80",
		RemoteSecurityGroup: &provision.SGRuleGroup{ID: "g-db", Name: "db"}, State: "created"}}
	for _, cidr := range []string{"0.0.0.0/0", "203.0.113.0/24"} {
		if covered, _ := Coverage(rules, "tcp", 80, cidr); covered {
			t.Errorf("port 80 from %s counts as open by a rule that lets in only security group db", cidr)
		}
	}
}
