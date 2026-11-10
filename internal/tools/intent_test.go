// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// Release plan 1.8, (и) («Риск», conclusion 46; cloud-e2e 181, R3-F1 2 of
// 2): kill -9 three seconds after the consent left a disk in the cloud and
// only the preview in the journal. The gate writes the intent after the
// consent and before the call goes out: a cut call leaves it — the tool,
// the names of the arguments, the resource name, no other value — an
// answered one removes it, and one that cannot be written goes nowhere.
func TestGate_IntentOfAConsentedCall(t *testing.T) {
	r, dir := targetRegistry(t, &goneCloud{})
	args := map[string]any{"name": "disk-1", "size": 10, "secret_hint": "s3cr3t-value", "confirmed": true}

	// Cut between the consent and the answer: the intent stays.
	ctx, cancel := context.WithCancel(context.Background())
	sent := false
	tool, _ := r.Lookup("disk_create")
	h := r.intended(tool, func(context.Context, map[string]any) (string, bool) {
		sent = true
		if got := intent.Pending(dir); len(got) != 1 {
			t.Errorf("the call went out with %d intents written", len(got))
		}
		cancel()
		return `{"reason":"INTERRUPTED"}`, true
	})
	h(ctx, args)
	got := intent.Pending(dir)
	if !sent || len(got) != 1 || got[0].Tool != "disk_create" || got[0].Name != "disk-1" || got[0].ProjectID != "p" ||
		strings.Join(got[0].Args, ",") != "name,secret_hint,size" {
		t.Fatalf("a cut call: %+v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(intent.Dir(dir), got[0].ID+".json"))
	if strings.Contains(string(raw), "s3cr3t-value") || strings.Contains(string(raw), `"size":`) {
		t.Errorf("a value of an argument in the intent: %s", raw)
	}

	// The same tool and name answered later: the cut one is settled, and
	// the answered call leaves none.
	h = r.intended(tool, func(context.Context, map[string]any) (string, bool) { return `{"id":"d-1","existing_id":"d-1"}`, false })
	h(context.Background(), args)
	if got := intent.Pending(dir); len(got) != 0 {
		t.Errorf("after an answer: %+v", got)
	}

	// Nowhere to write: refused before the call goes out.
	if err := os.RemoveAll(intent.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(intent.Dir(dir), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sent = false
	h = r.intended(tool, func(context.Context, map[string]any) (string, bool) { sent = true; return "{}", false })
	text, isErr := h(context.Background(), args)
	if sent || !isErr || !strings.Contains(text, config.DataDir(dir)) || !strings.Contains(text, `"reason": "CONFIG_INVALID"`) {
		t.Errorf("an intent that cannot be written: sent %v\n%s", sent, text)
	}
}

// The intent a cut call left is read: the first answer of the process over
// MCP carries it once, doctor names it, and the note says where to look,
// not what to delete (release plan 1.8, (и); «Риск», conclusion 46).
func TestPending_ReadOnBothSurfaces(t *testing.T) {
	r, dir := targetRegistry(t, &goneCloud{})
	t.Chdir(dir)
	if _, err := intent.Begin(dir, intent.Intent{Tool: "disk_create", Name: "disk-1", ProjectID: "p", Surface: "cli", PID: 42}); err != nil {
		t.Fatal(err)
	}
	notes := r.PendingNotes(dir)
	if len(notes) != 1 || !strings.Contains(notes[0], "disk_list") || !strings.Contains(notes[0], "disk-1") ||
		strings.Contains(strings.ToLower(notes[0]), "delete") {
		t.Fatalf("notes: %q", notes)
	}
	first, _ := r.handlers["vm_list"](context.Background(), map[string]any{})
	second, _ := r.handlers["vm_list"](context.Background(), map[string]any{})
	if !strings.Contains(first, "pending_operations") || strings.Contains(second, "pending_operations") {
		t.Errorf("the first MCP answer carries the intent once:\n%s\n%s", first, second)
	}
	// doctor's probes go to a local server: tests do not touch the network.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	oldIAM, oldCompute := auth.IAMEndpoint, ComputeBaseURL
	t.Cleanup(func() { auth.IAMEndpoint, ComputeBaseURL = oldIAM, oldCompute })
	auth.IAMEndpoint, ComputeBaseURL = srv.URL+"/token", srv.URL
	text, _ := r.handlers["doctor"](context.Background(), map[string]any{})
	if !strings.Contains(text, "pending operation") || !strings.Contains(text, "disk-1") {
		t.Errorf("doctor does not name the intent:\n%s", text)
	}
}
