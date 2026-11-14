// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// disk_create without attach_to waits for its disk as the attach does:
// created for a disk the platform made available, as before; creating for
// one still being made when the wait ends, with its state and disk_list of
// that disk (ids) as the next step — not created over a disk creating
// (Астра #1814; release plan 1.9, «Честный ход операций», (1); «Риск»,
// conclusion 227: the next step by the list's own ids filter).
func TestDiskCreate_ADiskStillBeingMadeIsNotCreated(t *testing.T) {
	a := &faultAPI{slow: true}
	r := diskWorldRegistry(t, a)
	text, isErr := r.handlers["disk_create"](context.Background(), map[string]any{
		"name": "data", "size_gb": 10, "zone": "ru.AZ-1", "confirmed": true,
	})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	next, _ := m["next_args"].(map[string]any)
	id, _ := m["disk_id"].(string)
	ids, _ := next["ids"].([]any)
	if isErr || m["status"] != "creating" || m["disk_state"] != "creating" || id == "" ||
		m["next_tool"] != "disk_list" || len(ids) != 1 || ids[0] != id {
		t.Fatalf("disk_create of a disk the platform still makes: %s", text)
	}
	text, isErr = r.handlers["disk_list"](context.Background(), next)
	if isErr || !strings.Contains(text, id) {
		t.Errorf("disk_list %v: %s", next, text)
	}
}

func TestDiskCreate_AnAvailableDiskIsCreated(t *testing.T) {
	a := &faultAPI{}
	r := diskWorldRegistry(t, a)
	text, isErr := r.handlers["disk_create"](context.Background(), map[string]any{
		"name": "data", "size_gb": 10, "zone": "ru.AZ-1", "confirmed": true,
	})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	if isErr || m["status"] != "created" || m["next_tool"] != nil {
		t.Errorf("disk_create of a disk the platform made: %s", text)
	}
}

// diskWorldRegistry is budgetAPIRegistry with the world's short waits: a
// disk still being made is waited for at the world's pace, not for the
// production five minutes.
func diskWorldRegistry(t *testing.T, a *faultAPI) *Registry {
	t.Helper()
	faultToolsIsolate(t)
	old := httpretry.DefaultBase
	httpretry.DefaultBase = a
	t.Cleanup(func() { httpretry.DefaultBase = old })
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: a}, "")
	cloud.Backoff = time.Millisecond
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: t.TempDir(),
			Timings:  faultToolsTimings(),
		}, nil
	})
	r.SetStateDir(t.TempDir())
	return r
}
