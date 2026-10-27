// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/journal"
)

func journalOn(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvConfigDir, filepath.Join(t.TempDir(), "evo"))
	if err := config.SetJournal(true); err != nil {
		t.Fatal(err)
	}
	journal.Begin(journal.SurfaceCLI, "test")
}

func journalRecords(t *testing.T) []journal.Record {
	t.Helper()
	f, err := os.Open(journal.Path())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []journal.Record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r journal.Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func noEngine(map[string]any) (*engine.Engine, error) {
	return nil, errors.New("no engine in this test")
}

// Every tool of the registry writes one record per call — the journal is
// the outermost layer of every handler (D46), so a tool without it, or a
// transport's Wrap that drops it, fails here. A call the check refuses is
// recorded too.
func TestJournal_EveryToolRecordsOneCall(t *testing.T) {
	journalOn(t)
	r := New(noEngine)
	r.Wrap("vm_list", func(next Handler) Handler { return next })
	names := map[string]bool{}
	for _, tl := range r.Tools() {
		names[tl.Name] = true
		tl.Handler(context.Background(), map[string]any{"no_such_argument_of_any_tool": 1})
	}
	got := map[string]int{}
	for _, rec := range journalRecords(t) {
		got[rec.Tool]++
		if rec.Depth != 0 || rec.UnknownArgs != 1 || len(rec.Args) != 0 || !rec.Error || rec.Reason != "USAGE" {
			t.Errorf("record of a refused call: %+v", rec)
		}
	}
	for n := range names {
		if got[n] != 1 {
			t.Errorf("%s: %d records, want 1", n, got[n])
		}
	}
}

// The journal holds names of the arguments the schema declares, never a
// value, and never the name of an argument the schema does not know (the
// caller's text). The answer is measured, not stored.
func TestJournal_NoValues(t *testing.T) {
	journalOn(t)
	r := New(noEngine)
	h := r.handlers["secret_create"]
	const value = "s3cr3t-value-0123456789"
	h(context.Background(), map[string]any{"path": value, "value": value})
	h(context.Background(), map[string]any{"path": value, value: value})
	data, err := os.ReadFile(journal.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), value) || strings.Contains(string(data), "s3cr3t") {
		t.Fatalf("a value got into the journal:\n%s", data)
	}
	rs := journalRecords(t)
	if len(rs) != 2 || strings.Join(rs[0].Args, ",") != "path,value" || rs[1].UnknownArgs != 1 || rs[0].Bytes == 0 {
		t.Errorf("records: %+v", rs)
	}
}

// A step a tool runs through the registry (the stack's callTool) is a
// record of its own, one level deeper.
func TestJournal_NestedCallIsDeeper(t *testing.T) {
	journalOn(t)
	r := New(noEngine)
	r.handlers["vm_list"](context.Background(), nil)
	outer := journaled("outer_test", nil, func(ctx context.Context, _ map[string]any) (string, bool) {
		return r.handlers["vm_list"](ctx, nil)
	})
	outer(context.Background(), nil)
	rs := journalRecords(t)
	if len(rs) != 3 || rs[0].Depth != 0 || rs[1].Tool != "vm_list" || rs[1].Depth != 1 || rs[2].Tool != "outer_test" || rs[2].Depth != 0 {
		t.Errorf("records: %+v", rs)
	}
}
