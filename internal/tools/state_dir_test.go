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

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// stateDirRegistry is a registry of engines built in dir and told
// declared (SetStateDir; "" tells it nothing); built counts its engines.
func stateDirRegistry(dir, declared string) (r *Registry, built *int) {
	built = new(int)
	r = New(func(map[string]any) (*engine.Engine, error) {
		*built++
		return &engine.Engine{Config: config.Config{ProjectID: "p", KeyID: "k", Secret: "s"}, StateDir: dir}, nil
	})
	if declared != "" {
		r.SetStateDir(declared)
	}
	return r, built
}

// A registry that can reach the cloud and was told no directory for the
// records of its calls is a defect of the build of evo: every tool that
// takes consent refuses — the preview as well as the consented call —
// before any engine is built, in words that do not send the person to
// repair a configuration that is not broken (release plan 1.9, «Каталог
// записей намерений»; «Риск», conclusion 172, У1).
func TestStateDir_UndeclaredRefusesEveryToolThatTakesConsent(t *testing.T) {
	r, built := stateDirRegistry(t.TempDir(), "")
	checked := 0
	for _, tool := range r.Tools() {
		if !takesConsent(tool) {
			continue
		}
		checked++
		for _, args := range []map[string]any{{}, {"confirmed": true}} {
			text, isErr := tool.Handler(context.Background(), args)
			var a struct{ Status, Reason, Error string }
			_ = json.Unmarshal([]byte(text), &a)
			if !isErr || a.Reason != clierrors.CodeConfigInvalid || !strings.Contains(a.Error, "a defect of evo, not of your configuration") ||
				a.Status == "confirmation_required" {
				t.Errorf("%s %v: not the refusal of a build with no directory:\n%s", tool.Name, args, text)
			}
		}
	}
	if checked < 60 || *built != 0 {
		t.Fatalf("%d tools that take consent checked, %d engines built: want every one and none", checked, *built)
	}
}

// A read of such a registry answers, and its first answer says the records
// of earlier calls were not read — not the silence that reads as «nothing
// was left» (conclusion 172, У3).
func TestStateDir_UndeclaredReadSaysTheRecordsWereNotRead(t *testing.T) {
	r, _ := stateDirRegistry(t.TempDir(), "")
	tool, _ := r.Lookup("agent_skill")
	text, isErr := tool.Handler(context.Background(), map[string]any{"group": "vm"})
	var a struct {
		Pending []string `json:"pending_operations"`
	}
	_ = json.Unmarshal([]byte(text), &a)
	if isErr || len(a.Pending) != 1 || !strings.Contains(a.Pending[0], "were not read") {
		t.Fatalf("isErr %v, pending_operations %v\n%s", isErr, a.Pending, text)
	}
}

// The records are of the declared directory, wherever the process stands:
// a consented call writes its record there, and the record of a cut call
// left there — by this build or the one before it, the file is the same —
// makes the same call ask again (conclusion 172, У5).
func TestStateDir_RecordsAreOfTheDeclaredDirectoryNotOfTheProcess(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	t.Chdir(elsewhere)
	r, _ := stateDirRegistry(dir, dir)
	reboot, _ := r.Lookup("vm_reboot")
	args := map[string]any{"vm_id": resMine, "confirmed": true}

	// A call whose answer was lost keeps its record — in dir.
	lost := r.intended(reboot, func(ctx context.Context, _ map[string]any) (string, bool) {
		markSent(ctx) // the command went out
		return `{"status":"error","error":"reboot: API_ERROR: 502","reason":"API_ERROR"}`, true
	})
	lost(context.Background(), args)
	if n := len(intent.Pending(intent.Dir(dir))); n != 1 {
		t.Fatalf("%d records in the declared directory, want 1", n)
	}
	if n := len(intent.Pending(intent.Dir(elsewhere))); n != 0 {
		t.Fatalf("%d records where the process stands, want none", n)
	}

	// The same call again, by a registry built anew (the next run): it
	// finds the record and asks, and sends nothing.
	next, _ := stateDirRegistry(dir, dir)
	sent := 0
	again := next.intended(reboot, func(ctx context.Context, args map[string]any) (string, bool) {
		// The layer asks the handler for the preview of its question
		// without the consent: only a consented call sends.
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			return `{"status":"error","reason":"USAGE"}`, true
		}
		markSent(ctx)
		sent++
		return `{"status":"reboot_accepted"}`, false
	})
	text, _ := again(context.Background(), args)
	if !strings.Contains(text, `"confirmation_required"`) || sent != 0 {
		t.Fatalf("the call after a cut one: sent %d\n%s", sent, text)
	}
}

// A record a build before this one left — a file of <dir>/.evo/intents, as
// that build wrote it from the engine's StateDir — is read by this one:
// the path did not move, only where the directory is taken from.
func TestStateDir_ARecordOfTheBuildBeforeIsRead(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(t.TempDir())
	old := intent.Intent{Tool: "vm_reboot", Target: "vm_id=" + resMine, Key: &state.Maker{KeyID: config.MaskKeyID("k")}}
	if _, err := intent.Open(filepath.Join(dir, ".evo", "intents"), old); err != nil {
		t.Fatal(err)
	}
	r, _ := stateDirRegistry(dir, dir)
	reboot, _ := r.Lookup("vm_reboot")
	sent := 0
	call := r.intended(reboot, func(ctx context.Context, args map[string]any) (string, bool) {
		// The layer asks the handler for the preview of its question
		// without the consent: only a consented call sends.
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			return `{"status":"error","reason":"USAGE"}`, true
		}
		markSent(ctx)
		sent++
		return `{"status":"reboot_accepted"}`, false
	})
	text, _ := call(context.Background(), map[string]any{"vm_id": resMine, "confirmed": true})
	if !strings.Contains(text, `"confirmation_required"`) || sent != 0 {
		t.Fatalf("a record of the build before is not read: sent %d\n%s", sent, text)
	}
}

// The first answer names the records of the declared directory when no
// engine can be built — a broken configuration is when the person needs
// them most, and the directory does not come from an engine.
func TestStateDir_PendingNotesWithNoEngineToBuild(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(t.TempDir())
	if _, err := intent.Open(intent.Dir(dir), intent.Intent{Tool: "vm_reboot", Target: "vm_id=" + resMine}); err != nil {
		t.Fatal(err)
	}
	r := New(func(map[string]any) (*engine.Engine, error) {
		return nil, clierrors.New(clierrors.CodeConfigInvalid, "the configuration is broken", "")
	})
	r.SetStateDir(dir)
	tool, _ := r.Lookup("whoami")
	text, _ := tool.Handler(context.Background(), map[string]any{})
	var a struct {
		Pending []string `json:"pending_operations"`
	}
	_ = json.Unmarshal([]byte(text), &a)
	if len(a.Pending) != 1 || !strings.Contains(a.Pending[0], "vm_reboot") {
		t.Fatalf("pending_operations %v\n%s", a.Pending, text)
	}
}

// A declared directory that is gone when the call comes (a long-lived
// server whose directory was deleted): the consented call is refused when
// its record cannot be written — it is not sent unrecorded.
func TestStateDir_ADirectoryThatCannotTakeTheRecordRefusesTheCall(t *testing.T) {
	// A file where the directory of the records would be: nothing can be
	// written under it.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".evo"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, _ := stateDirRegistry(dir, dir)
	reboot, _ := r.Lookup("vm_reboot")
	sent := 0
	call := r.intended(reboot, func(ctx context.Context, args map[string]any) (string, bool) {
		// The layer asks the handler for the preview of its question
		// without the consent: only a consented call sends.
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			return `{"status":"error","reason":"USAGE"}`, true
		}
		markSent(ctx)
		sent++
		return `{"status":"reboot_accepted"}`, false
	})
	text, isErr := call(context.Background(), map[string]any{"vm_id": resMine, "confirmed": true})
	if !isErr || sent != 0 {
		t.Fatalf("a call whose record cannot be written was sent (%d): %s", sent, text)
	}
}

// SetStateDir has no path from a call: no argument of a tool and no
// override of the engine factory changes the directory of the records
// (conclusion 172, У4).
func TestStateDir_NoArgumentOfACallMovesIt(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	r := New(func(over map[string]any) (*engine.Engine, error) {
		if len(over) > 0 {
			// An engine built with overrides stands elsewhere: the
			// records do not follow it.
			return &engine.Engine{Config: config.Config{ProjectID: "p2", KeyID: "k", Secret: "s"}, StateDir: other}, nil
		}
		return &engine.Engine{Config: config.Config{ProjectID: "p", KeyID: "k", Secret: "s"}, StateDir: dir}, nil
	})
	r.SetStateDir(dir)
	reboot, _ := r.Lookup("vm_reboot")
	call := r.intended(reboot, func(ctx context.Context, _ map[string]any) (string, bool) {
		markSent(ctx)                                                                           // the command went out
		return `{"status":"error","error":"reboot: API_ERROR: 502","reason":"API_ERROR"}`, true // a lost answer: the record stays
	})
	call(context.Background(), map[string]any{"vm_id": resMine, "project_id": "p2", "confirmed": true})
	if a, b := len(intent.Pending(intent.Dir(dir))), len(intent.Pending(intent.Dir(other))); a != 1 || b != 0 {
		t.Fatalf("records: %d in the declared directory, %d in another; want 1 and 0", a, b)
	}
}
