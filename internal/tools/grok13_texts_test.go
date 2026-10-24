// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// G13-19 (Grok, wave 2): kafka_delete of a cluster in FAILED answered a bare
// 500, and its preview said nothing. The preview warns that Cloud.ru may
// refuse the delete in that state, and the 500 says what to do next; the
// reason stays API_ERROR.
func TestDeletePreview_WarnsInFailedState(t *testing.T) {
	for _, status := range []string{"FAILED", "ERROR", "error"} {
		text := toJSON(New(nil).deletePlan("kafka_delete", "x_delete deletes %s.", "x_delete удаляет %s.", "id-1", func() (map[string]any, error) {
			return map[string]any{"name": "c1", "status": status}, nil
		}, nil).Preview)
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatal(err)
		}
		w, _ := m["warning"].(string)
		if !strings.Contains(w, "support") || !strings.Contains(w, "id-1") {
			t.Errorf("status %s: warning %q, want the refusal and support with the id", status, w)
		}
	}
}

func TestKafkaDelete_ServerErrorSaysWhatNext(t *testing.T) {
	// The cluster is there (its plan reads it first, D35); its delete
	// answers 500.
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"message":"upstream"}`)
			return true
		}
		return liveWorld(w, r)
	}}
	r := apiFakeS3Registry(t, f)
	text, isErr := r.handlers["kafka_delete"](context.Background(), map[string]any{"cluster_id": liveID, "confirmed": true})
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || !isErr {
		t.Fatalf("kafka_delete: %s", text)
	}
	if m["reason"] != "API_ERROR" {
		t.Errorf("reason %v, want API_ERROR (unchanged): %s", m["reason"], text)
	}
	// The same 500 answers a cluster still being created (DEPLOYING,
	// register DB-17): wait for READY and repeat; a FAILED one goes to
	// support with its id.
	if h, _ := m["hint"].(string); !strings.Contains(h, "READY") || !strings.Contains(h, "support") || !strings.Contains(h, liveID) {
		t.Errorf("hint %q, want wait for READY, or support with the cluster id: %s", m["hint"], text)
	}
}

// G13-7: deploy_plan of a compose project said "call deploy_apply", which
// makes the VM without the application. next says so and names deploy.
func TestDeployPlan_ComposeNextNamesDeploy(t *testing.T) {
	r, _ := planRegistry(t, config.Config{Region: planZoneAZ1, Flavor: "gen-4-8", ImageID: "img-d"})
	compose := filepath.Join(t.TempDir(), "docker-compose.yml")
	if err := os.WriteFile(compose, []byte("services:\n  web:\n    image: nginx:1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args    map[string]any
		compose bool
	}{
		{map[string]any{"name": "web"}, false},
		{map[string]any{"name": "web", "compose_file": compose}, true},
	} {
		text, isErr := r.handlers["deploy_plan"](context.Background(), tc.args)
		if isErr {
			t.Fatalf("deploy_plan: %s", text)
		}
		var view struct {
			Next    string `json:"next"`
			Compose bool   `json:"compose_required"`
		}
		if err := json.Unmarshal([]byte(text), &view); err != nil {
			t.Fatal(err)
		}
		says := strings.Contains(view.Next, "without the application") && strings.Contains(view.Next, "deploy instead")
		if view.Compose != tc.compose || says != tc.compose {
			t.Errorf("compose %v: compose_required %v, next %q", tc.compose, view.Compose, view.Next)
		}
	}
}
