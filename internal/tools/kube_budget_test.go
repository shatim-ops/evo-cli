// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kubectl stopped at the limit of an MCP call (D69) answers what it printed
// and what holds: a read is repeated (next_tool), a change is checked with
// a read before it is repeated — what kubectl sent goes on in the cluster.
// Which is which is kubectlReadOnly's, the gate of consent's own rule.
func TestKubectl_StoppedAtTheLimitSaysWhatHolds(t *testing.T) {
	for _, tc := range []struct {
		args   string
		repeat bool
	}{
		{"rollout status deployment/web -n app", true},
		{"wait --for=condition=Ready pod/web -n app", true},
		{"delete deployment web -n app", false},
		// Not idempotent: a repeat restarts again, runs the command again
		// («1.7 — релиз» on PR 5 of D69).
		{"rollout restart deployment/web -n app", false},
		{"exec web -n app -- migrate", false},
	} {
		t.Run(tc.args, func(t *testing.T) {
			args := map[string]any{"cluster_id": faultCluster, "args": tc.args, "confirmed": true}
			// progressBudget, not the budget of the slow world: the test
			// reads how far kubectl got — the line it printed — and on a
			// loaded runner the process does not start and print within
			// 250 ms (macOS, run 43267821110: "output": "", #793's class).
			r := progressed(kubeWorld(t, `echo "$(basename "$0"): waiting"; exec sleep 600`))
			text, isErr := r.handlers["k8s_kubectl"](context.Background(), args)
			var m map[string]any
			if err := json.Unmarshal([]byte(text), &m); err != nil || isErr {
				t.Fatalf("answer (isError=%v): %s", isErr, text)
			}
			if m["status"] != "stopped_at_limit" || !strings.Contains(m["output"].(string), "kubectl: waiting") {
				t.Errorf("status and output so far: %s", text)
			}
			if _, has := m["next_tool"]; has != tc.repeat {
				t.Errorf("next_tool present = %v, want %v: %s", has, tc.repeat, text)
			}
			if next, _ := m["next"].(string); !strings.Contains(next, "k8s_kubectl") {
				t.Errorf("next names no tool: %s", text)
			}
		})
	}
}

// helmArgs are the arguments the world's helm was last run with.
func helmArgs(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("KUBE_ARGS"), "helm"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

var helmInstall = map[string]any{"cluster_id": faultCluster, "action": "install", "release": "web", "chart": "repo/web", "namespace": "app", "confirmed": true}

// Over MCP helm install does not wait for the pods (D69, variant (a) of
// «1.7 — релиз»): helm says deployed once the cluster took the manifests;
// the answer is deploying — not ok — and the next step reads the release.
// helm's own --timeout ends before the call's budget, so helm ends a hook
// that hangs itself instead of being killed mid-operation.
func TestHelm_InstallOverMCPDoesNotWaitAndSaysDeploying(t *testing.T) {
	// helm has to finish here: progressBudget, not worldBudget (#793) —
	// 250 ms did not hold for the fork of the world's helm on a slow macOS
	// runner (#1246), and the answer was stopped_at_limit.
	r := progressed(kubeWorld(t, `echo "STATUS: deployed"`))
	text, isErr := r.handlers["k8s_helm"](context.Background(), helmInstall)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || isErr {
		t.Fatalf("answer (isError=%v): %s", isErr, text)
	}
	if m["status"] != "deploying" || m["next_tool"] != "k8s_helm" {
		t.Errorf("status and next step: %s", text)
	}
	if next, _ := m["next_args"].(map[string]any); next["action"] != "status" || next["release"] != "web" || next["namespace"] != "app" {
		t.Errorf("next_args read the release: %v", m["next_args"])
	}
	args := strings.Join(helmArgs(t), " ")
	if strings.Contains(args, "--wait") || !strings.Contains(args, "--timeout") || strings.Contains(args, "--timeout 8m") {
		t.Errorf("helm ran with %q: no --wait, its own --timeout within the budget", args)
	}
}

// helm that fails by itself (a chart that does not render, a conflict) is
// an error, not deploying («1.7 — релиз»).
func TestHelm_AFailingInstallOverMCPIsAnError(t *testing.T) {
	r := progressed(kubeWorld(t, `echo "Error: UPGRADE FAILED: rendered manifests contain a resource that already exists"; exit 1`))
	text, isErr := r.handlers["k8s_helm"](context.Background(), helmInstall)
	if !isErr || strings.Contains(text, "deploying") {
		t.Errorf("a failing helm reads (isError=%v): %s", isErr, text)
	}
}

// helm that hangs (a hook that never ends) is stopped at the limit of the
// call: not an error; the release may be left mid-operation, and the next
// step reads it.
func TestHelm_StoppedAtTheLimitReadsTheRelease(t *testing.T) {
	call, _ := kubeBudget("k8s_helm", helmInstall)(t)
	text, isErr := call(context.Background())
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || isErr {
		t.Fatalf("answer (isError=%v): %s", isErr, text)
	}
	next, _ := m["next_args"].(map[string]any)
	if m["status"] != "stopped_at_limit" || next["action"] != "status" || !strings.Contains(m["next"].(string), "rollback") {
		t.Errorf("a hung helm: %s", text)
	}
}

// The CLI waits as before (H14): --wait --timeout 8m.
func TestHelm_InstallInTheCLIWaitsAsBefore(t *testing.T) {
	r := kubeWorld(t, `echo "STATUS: deployed"`)
	text, isErr := r.handlers["k8s_helm"](context.Background(), helmInstall)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || isErr || m["status"] != "ok" {
		t.Fatalf("answer (isError=%v): %s", isErr, text)
	}
	if args := strings.Join(helmArgs(t), " "); !strings.Contains(args, "--wait --timeout 8m") {
		t.Errorf("helm ran with %q, want --wait --timeout 8m", args)
	}
}

// helm stopped in repo add or update — before it touched the release — is
// asked again with the same arguments.
func TestHelm_StoppedInTheRepoStepRepeatsTheCall(t *testing.T) {
	r := budgeted(kubeWorld(t, `[ "$1" = repo ] && exec sleep 600; echo "STATUS: deployed"`))
	args := map[string]any{"cluster_id": faultCluster, "action": "install", "release": "web", "chart": "repo/web", "repo_url": "https://charts.example.com", "confirmed": true}
	text, isErr := r.handlers["k8s_helm"](context.Background(), args)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || isErr {
		t.Fatalf("answer (isError=%v): %s", isErr, text)
	}
	next, _ := m["next_args"].(map[string]any)
	if m["status"] != "stopped_at_limit" || m["stage"] != "repo add" || next["action"] != "install" || next["repo_url"] != "https://charts.example.com" {
		t.Errorf("stopped in repo add: %s", text)
	}
}
