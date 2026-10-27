// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/journal"
)

// Whatever a server mode wraps (--read-only, the HTTP restrictions), a
// call of a tool is one record of the session journal (D46): the journal
// is the outermost layer, a mode's wrap goes under it.
func TestJournal_OneRecordPerCallInEveryMode(t *testing.T) {
	orig := binaryAvailable
	t.Cleanup(func() { binaryAvailable = orig })
	binaryAvailable = func(string) bool { return false }
	for mode, build := range map[string]func() *Server{
		"read-only over http": func() *Server { s := newTestServer(); s.SetReadOnly(); s.restrictForHTTP(false); return s },
		"http":                func() *Server { s := newTestServer(); s.restrictForHTTP(true); return s },
	} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv(config.EnvConfigDir, filepath.Join(t.TempDir(), "evo"))
			if err := config.SetJournal(true); err != nil {
				t.Fatal(err)
			}
			journal.Begin(journal.SurfaceMCPHTTP, "test")
			s := build()
			n := 0
			for _, tl := range s.reg.Tools() {
				call(s, tl.Name, map[string]any{"no_such_argument_of_any_tool": 1})
				n++
			}
			f, err := os.Open(journal.Path())
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got := map[string]int{}
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				var r journal.Record
				if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
					t.Fatal(err)
				}
				got[r.Tool]++
			}
			for _, tl := range s.reg.Tools() {
				if got[tl.Name] != 1 {
					t.Errorf("%s: %d records, want 1", tl.Name, got[tl.Name])
				}
			}
			if len(got) != n {
				t.Errorf("records of %d tools, called %d", len(got), n)
			}
		})
	}
}
