// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"maps"
	"strings"
	"testing"
)

// The refusals the planners of k8s_kubectl, k8s_apply and k8s_helm make
// are the same with and without consent, and neither call writes or asks
// for the kubeconfig (a POST): the kubeconfig is read by the execution,
// after the question (the inventory of the gate, variant А). A call that
// needs no consent — a read command — is refused the same way.
func TestGateKube_RefusalWithoutConsentIsTheRefusalWithIt(t *testing.T) {
	for _, c := range []struct {
		name, tool string
		args       map[string]any
	}{
		{"no args", "k8s_kubectl", map[string]any{"cluster_id": resMine}},
		{"another server, a change", "k8s_kubectl", map[string]any{"cluster_id": resMine, "args": "delete pod web --server=https://example.invalid"}},
		{"another server, a read", "k8s_kubectl", map[string]any{"cluster_id": resMine, "args": "get pods --server=https://example.invalid"}},
		{"no manifest", "k8s_apply", map[string]any{"cluster_id": resMine}},
		{"namespace is a flag", "k8s_apply", map[string]any{"cluster_id": resMine, "manifest": "kind: Namespace", "namespace": "-x"}},
		{"no action", "k8s_helm", map[string]any{"cluster_id": resMine}},
		{"unknown action", "k8s_helm", map[string]any{"cluster_id": resMine, "action": "rollback", "release": "web"}},
		{"install with no chart", "k8s_helm", map[string]any{"cluster_id": resMine, "action": "install", "release": "web"}},
		{"release is a flag", "k8s_helm", map[string]any{"cluster_id": resMine, "action": "uninstall", "release": "--post-renderer"}},
		{"relative repo_url", "k8s_helm", map[string]any{"cluster_id": resMine, "action": "install", "release": "web", "chart": "r/web", "repo_url": "charts/"}},
		{"status with no release", "k8s_helm", map[string]any{"cluster_id": resMine, "action": "status"}},
	} {
		var answers [2]string
		for i, confirmed := range []bool{false, true} {
			f := &ownerFake{}
			r := ownerRegistry(t, f, false, ownProject)
			m, text, isErr := callOwned(t, r, c.tool, maps.Clone(c.args), confirmed)
			if !isErr || m["status"] != "error" || m["reason"] != "USAGE" {
				t.Errorf("%s, %s (confirmed %v): %s, want a refusal USAGE", c.tool, c.name, confirmed, text)
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
