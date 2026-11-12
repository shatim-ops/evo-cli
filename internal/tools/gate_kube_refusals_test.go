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

// A step that repeats a read cut at the limit of an MCP call carries no
// consent, whatever the call was given: a read asks for none («Риск»,
// conclusion 126). The step of k8s_helm stopped at repo add or repo
// update, before the release was touched, keeps the consent of its call:
// a declared continuation (owner's decision 12.11).
func TestKubeAtLimit_ReadStepCarriesNoConsent(t *testing.T) {
	stepArgs := func(answer map[string]any) map[string]any {
		t.Helper()
		next, _ := answer["next_args"].(map[string]any)
		if next == nil {
			t.Fatalf("no next_args in %v", answer)
		}
		return next
	}
	kubectl := map[string]any{"cluster_id": resMine, "args": "rollout status deploy/web", "confirmed": true}
	if next := stepArgs(kubectlAtLimit("", "rollout", kubectl)); next["confirmed"] != nil || next["args"] != kubectl["args"] {
		t.Errorf("the step of a kubectl read: %v", next)
	}
	for _, action := range []string{"list", "status"} {
		helm := map[string]any{"cluster_id": resMine, "action": action, "release": "web", "confirmed": true}
		if next := stepArgs(helmAtLimit("", action, helm)); next["confirmed"] != nil || next["action"] != action {
			t.Errorf("the step of helm %s: %v", action, next)
		}
	}
	install := map[string]any{"cluster_id": resMine, "action": "install", "release": "web", "chart": "r/web", "repo_url": "https://charts.example.invalid", "confirmed": true}
	if next := stepArgs(helmAtLimit("", "repo add", install)); next["confirmed"] != true {
		t.Errorf("the step of helm stopped at repo add lost the consent of its call: %v", next)
	}
	if kubectl["confirmed"] != true || install["confirmed"] != true {
		t.Error("the step changed the arguments of the call")
	}
}
