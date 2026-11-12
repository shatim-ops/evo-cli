// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"maps"
	"strings"
	"testing"
)

// The refusals of k8s_create and k8s_add_nodepool are the planner's: a
// call that is refused without consent is refused with confirmed=true by
// the same answer, and neither writes (conclusion 119 of «Риск»: a refusal
// that lived in the preview alone would be skipped by a consented call).
// The cases walk the planner's checks: the arguments, the taken name of a
// cluster, the zone of the cluster, a subnet of another project.
func TestGateK8s_RefusalWithoutConsentIsTheRefusalWithIt(t *testing.T) {
	for _, c := range []struct {
		name, tool string
		args       map[string]any
		reason     string
	}{
		{"master_count out of the set", "k8s_create", map[string]any{"name": "cl-1", "master_count": float64(2)}, "USAGE"},
		{"a name the specification refuses", "k8s_create", map[string]any{"name": "a b"}, "USAGE"},
		{"the name is taken", "k8s_create", map[string]any{"name": "res-a1"}, "USAGE"},
		{"a subnet of another project", "k8s_create", map[string]any{"name": "cl-1", "subnet_id": resForeign}, ""},
		{"no cluster_id", "k8s_add_nodepool", map[string]any{"name": "np-1"}, "USAGE"},
		{"the cluster reports no zone", "k8s_add_nodepool", map[string]any{"cluster_id": resMine, "name": "np-1"}, "CONFIG_INVALID"},
		{"a subnet of another project", "k8s_add_nodepool", map[string]any{"cluster_id": resMine, "name": "np-1", "zone": "ru.AZ-1", "nodes_subnet_id": resForeign}, ""},
	} {
		var answers [2]string
		for i, confirmed := range []bool{false, true} {
			f := &ownerFake{}
			r := ownerRegistry(t, f, false, ownProject)
			m, text, isErr := callOwned(t, r, c.tool, maps.Clone(c.args), confirmed)
			if !isErr || m["status"] != "error" || (c.reason != "" && m["reason"] != c.reason) {
				t.Errorf("%s, %s (confirmed %v): %s, want a refusal %s", c.tool, c.name, confirmed, text, c.reason)
			}
			for _, q := range f.log() {
				if !strings.HasPrefix(q, "GET ") {
					t.Errorf("%s, %s (confirmed %v): the refused call sent %s", c.tool, c.name, confirmed, q)
				}
			}
			answers[i] = text
		}
		if answers[0] != answers[1] {
			t.Errorf("%s, %s: without consent\n%s\nwith confirmed=true\n%s", c.tool, c.name, answers[0], answers[1])
		}
	}
}
