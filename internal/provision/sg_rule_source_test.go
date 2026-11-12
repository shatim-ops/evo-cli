// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"strings"
	"testing"
)

// The source of a rule is a prefix or a group (PublicSecurityGroupRuleResponse:
// remote_ip_prefix, remote_security_group{id, name}). A rule read without
// its group reads as a rule «from anywhere» — the opposite of what it is.
// The field is read only: a request does not carry it.
func TestSGRule_ReadsItsSourceGroup(t *testing.T) {
	var r SGRule
	body := `{"id":"00000000-0000-4000-8000-000000000e03","direction":"ingress","ether_type":"IPv4","ip_protocol":"tcp","port_range":"22:22",` +
		`"remote_security_group":{"id":"00000000-0000-4000-8000-0000000000e2","name":"db"},"state":"created"}`
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	if r.RemoteSecurityGroup == nil || r.RemoteSecurityGroup.ID != "00000000-0000-4000-8000-0000000000e2" || r.RemoteSecurityGroup.Name != "db" || r.RemoteIPPrefix != "" {
		t.Fatalf("the rule's source group: %+v", r)
	}
	sent, err := json.Marshal(SGRule{Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "80:80", RemoteIPPrefix: "0.0.0.0/0"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sent), "remote_security_group") {
		t.Errorf("a request carries the read-only field: %s", sent)
	}
}
