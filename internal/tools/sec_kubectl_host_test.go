// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// SEC-4: k8s_kubectl по stdio. Глагол чтения с -f/--filename, -k/--kustomize
// или --output-directory выходит за пределы кластера: «get -f https://…» —
// запрос на любой URL, «get -f <файл>» печатает файл в ошибке, «cluster-info
// dump --output-directory» пишет на хост. Такой вызов требует confirmed.
func TestKubectlReadOnly_HostFlags(t *testing.T) {
	for _, c := range []struct {
		args string
		ro   bool
	}{
		{"get pods -A", true},
		{"get pods -n kube-system -o yaml", true},
		{"logs -f deploy/web", true}, // -f у logs — это --follow
		{"logs deploy/web --follow", true},
		{"rollout status deploy/web", true},
		{"get -f https://example.invalid/x.yaml", false},
		{"get -f /etc/passwd", false},
		{"get -f=/etc/passwd", false},
		{"get -f/etc/passwd", false},
		{"get -fx", false},
		{"get -Af x", false},
		{"get --filename=/etc/passwd", false},
		{"get --filename /etc/passwd", false},
		{"describe -f manifests/", false},
		{"diff -f app.yaml", false},
		{"wait --for=condition=ready -f pod.yaml", false},
		{"rollout status -f deploy.yaml", false},
		{"get -k ./overlay", false},
		{"get -k=./overlay", false},
		{"get --kustomize=./overlay", false},
		{"cluster-info dump --output-directory /tmp/x", false},
		{"cluster-info dump --output-directory=/tmp/x", false},
		{"cluster-info dump --output_directory=/tmp/x", false},
		{"cluster-info", true},
		// После «--» слова — не флаги kubectl.
		{"get pods -- -f", true},
	} {
		if got := kubectlReadOnly(strings.Fields(c.args)); got != c.ro {
			t.Errorf("%q: read-only = %v, want %v", c.args, got, c.ro)
		}
	}
}

// Через инструмент: без confirmed — confirmation_required, кластер не
// трогается (в облако не ходим).
func TestK8sKubectl_HostFlagNeedsConfirmed(t *testing.T) {
	f := &apiFake{}
	r := apiFakeRegistry(t, f)
	for _, args := range []string{"get -f https://example.invalid/x.yaml", "cluster-info dump --output-directory=/tmp/x"} {
		m, text, _ := callNamed(t, r, "k8s_kubectl", map[string]any{"cluster_id": "00000000-0000-0000-0000-000000000001", "args": args})
		if m["status"] != "confirmation_required" {
			t.Errorf("%q: %s", args, text)
		}
	}
	if n := f.count(); n != 0 {
		t.Errorf("%d requests before consent", n)
	}
}
