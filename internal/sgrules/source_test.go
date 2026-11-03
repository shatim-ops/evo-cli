// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// P5 (G2-2): the rules of a call open its ports to the cidr it was given,
// Anywhere without one. A rule from that source or wider already opens a
// port; one from a narrower source does not.
func TestOpen_OpensToTheSourceGiven(t *testing.T) {
	f := newFake()
	f.rules["sg-1"] = []provision.SGRule{
		{ID: "office-5432", Direction: "ingress", IPProtocol: "tcp", PortRange: "5432:5432", RemoteIPPrefix: "10.1.0.0/16"},
		{ID: "any-443", Direction: "ingress", IPProtocol: "tcp", PortRange: "443:443", RemoteIPPrefix: Anywhere},
	}
	var rec state.DeploymentRecord
	res := Open(context.Background(), f, "proj", "sg-1", []int{22, 443, 5432}, nil, "10.0.0.0/8", desc, &rec)
	if strings.Join(res.Opened, ",") != "22,5432" || strings.Join(res.AlreadyOpen, ",") != "443" {
		t.Fatalf("opened %v, already open %v; want 22,5432 opened and 443 (open wider) left", res.Opened, res.AlreadyOpen)
	}
	for _, r := range f.rules["sg-1"][2:] {
		if r.RemoteIPPrefix != "10.0.0.0/8" {
			t.Errorf("rule %s/%s opens to %q, want the cidr given", r.PortRange, r.IPProtocol, r.RemoteIPPrefix)
		}
	}

	g := newFake()
	var rec2 state.DeploymentRecord
	Open(context.Background(), g, "proj", "sg-1", []int{80}, nil, "", desc, &rec2)
	if got := g.rules["sg-1"][0].RemoteIPPrefix; got != Anywhere {
		t.Errorf("without a cidr the rule opens to %q, want %s", got, Anywhere)
	}
}

// A port other than 80 and 443 open to anyone is warned of, with the way
// to narrow it; a narrower source or the web's ports are not.
func TestWideOpen_WarnsOfPortsBesidesTheWeb(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tcp, udp []int
		cidr     string
		want     string // "" — no warning
	}{
		{"web only", []int{80, 443}, nil, "", ""},
		{"ssh and a database to anyone", []int{80, 22, 5432}, nil, "", "22,5432"},
		{"udp to anyone", nil, []int{53}, Anywhere, "53/udp"},
		{"a narrower source", []int{22}, nil, "10.0.0.0/8", ""},
		{"no ports", nil, nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := WideOpen(tc.tcp, tc.udp, tc.cidr)
			if tc.want == "" {
				if w != "" {
					t.Fatalf("warning %q, want none", w)
				}
				return
			}
			if !strings.Contains(w, tc.want+" ") || !strings.Contains(w, Anywhere) || !strings.Contains(w, "cidr") {
				t.Errorf("warning %q: want ports %s, the source and the way out (cidr)", w, tc.want)
			}
		})
	}
}
