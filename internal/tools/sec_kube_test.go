// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// k8s_apply put namespace after "-n" unchecked, unlike k8s_helm.
func TestSec_ApplyRejectsFlagAsNamespace(t *testing.T) {
	r := newTestServer()
	got := callTool(t, r.handleK8sApply, map[string]any{
		"cluster_id": "c", "manifest": "kind: Namespace", "namespace": "--kubeconfig=/etc/x", "confirmed": true,
	})
	if msg, _ := got["error"].(string); !strings.Contains(msg, "must not start with '-'") {
		t.Errorf("namespace as a flag: %v", got)
	}
}
