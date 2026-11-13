// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"strings"
	"testing"
)

// The cluster of k8s-smoke is deleted by k8s-delete.sh, which repeats a
// refused delete when the state of the cluster changes and names what is
// left; with the probe Г-12 the wait covers an upgrade of the version too,
// and the project is counted before the cluster and after its delete: what
// the service itself makes (nodes, their disks, groups, balancers) no
// guard by the cluster's name sees (risk note 189, У2, У3). One delete and
// thirty minutes of reading left a billable cluster when the platform
// refused it once.
func TestWorkflows_K8sClusterDeleteOutlastsTheProbe(t *testing.T) {
	j, ok := readWorkflows(t)["cloud-e2e.yml"].Jobs["k8s-smoke"]
	if !ok {
		t.Fatal("cloud-e2e: no job k8s-smoke")
	}
	var del, save, check bool
	saveAt, createAt := -1, -1
	for i, s := range j.Steps {
		switch {
		case strings.Contains(s.Run, "k8s-delete.sh"):
			del = true
			if !strings.HasPrefix(s.If, "always()") {
				t.Errorf("k8s-smoke, %s: the delete runs only with if: always(), got %q", s.Name, s.If)
			}
			w := s.Env["K8S_DEL_WAIT"]
			if !strings.Contains(w, "g12") || !strings.Contains(w, "4200") {
				t.Errorf("k8s-smoke, %s: K8S_DEL_WAIT does not give the probe g12 the wait of an upgrade (4200 s): %q", s.Name, w)
			}
		case strings.Contains(s.Run, "k8s-footprint.sh save"):
			save, saveAt = true, i
		case strings.Contains(s.Run, "k8s-footprint.sh check"):
			check = true
			if !strings.HasPrefix(s.If, "always()") {
				t.Errorf("k8s-smoke, %s: the count after the delete runs only with if: always(), got %q", s.Name, s.If)
			}
		}
		if strings.Contains(s.Run, "k8s create") && createAt < 0 {
			createAt = i
		}
	}
	if !del {
		t.Error("k8s-smoke: the cluster is not deleted by k8s-delete.sh")
	}
	if !save || !check {
		t.Error("k8s-smoke: no count of the project before the cluster and after its delete (k8s-footprint.sh save, check)")
	}
	if save && createAt >= 0 && saveAt > createAt {
		t.Error("k8s-smoke: the count of the project comes after the cluster is created")
	}
	// 40 (cluster) + 30 (pool) + 35 (checks) + 90 (probe) + 70 (delete).
	if j.TimeoutMinutes < 265 {
		t.Errorf("k8s-smoke: timeout-minutes %d leaves no room for the delete after the probe g12", j.TimeoutMinutes)
	}
}
