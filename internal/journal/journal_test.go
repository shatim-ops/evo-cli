// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvConfigDir, filepath.Join(t.TempDir(), "evo"))
	mu.Lock()
	old := current
	current.id = ""
	mu.Unlock()
	t.Cleanup(func() { mu.Lock(); current = old; mu.Unlock() })
}

func records(t *testing.T) []Record {
	t.Helper()
	f, err := os.Open(Path())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("a line of the journal is not JSON: %v: %q", err, sc.Text())
		}
		out = append(out, r)
	}
	return out
}

// Off — the default — writes nothing; on, a record carries the session.
func TestWrite_OnlyWhenOnAndBegun(t *testing.T) {
	isolate(t)
	if Active() {
		t.Fatal("active before a session began")
	}
	Write(Record{Tool: "vm_list"})
	Begin(SurfaceCLI, "1.4.0-test")
	if Active() {
		t.Fatal("the journal is off by default, yet active")
	}
	if _, err := os.Stat(Path()); !os.IsNotExist(err) {
		t.Fatalf("no session, yet the file is there: %v", err)
	}
	if err := config.SetJournal(true); err != nil {
		t.Fatal(err)
	}
	if !Active() {
		t.Fatal("on, yet not active")
	}
	Write(Record{Tool: "vm_list", Args: []string{"zone"}})
	rs := records(t)
	if len(rs) != 1 {
		t.Fatalf("records: %+v", rs)
	}
	r := rs[0]
	if r.Tool != "vm_list" || r.Evo != "1.4.0-test" || r.Surface != SurfaceCLI || len(r.Session) != 12 || r.Time == "" || r.OS == "" {
		t.Errorf("record without its session: %+v", r)
	}
	if fi, err := os.Stat(Path()); err == nil && config.PermissionsEnforced() && fi.Mode().Perm() != 0o600 {
		t.Errorf("journal mode %v, want 0600", fi.Mode().Perm())
	}
	Begin(SurfaceMCPStdio, "1.4.0-test")
	Write(Record{Tool: "vm_list"})
	if rs := records(t); len(rs) != 2 || rs[1].Session == rs[0].Session || rs[1].Surface != SurfaceMCPStdio {
		t.Errorf("a new session must have its own id and surface: %+v", rs)
	}
}

// A full journal says so once, in a record of its own, and takes no more.
func TestWrite_FullJournalSaysSo(t *testing.T) {
	isolate(t)
	old := limit
	limit = 4096
	t.Cleanup(func() { limit = old })
	Begin(SurfaceCLI, "t")
	if err := config.SetJournal(true); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		Write(Record{Tool: "vm_list", Args: []string{"zone", "project_id"}})
	}
	rs := records(t)
	full := 0
	for _, r := range rs {
		if r.Event == EventFull {
			full++
		}
	}
	if full != 1 || rs[len(rs)-1].Event != EventFull {
		t.Fatalf("want one %s record, the last: %d of %d", EventFull, full, len(rs))
	}
	if fi, _ := os.Stat(Path()); fi.Size() > limit {
		t.Errorf("journal %d bytes over the limit %d", fi.Size(), limit)
	}
	s, err := Describe()
	if err != nil || !s.Full || s.Records != len(rs)-1 || s.Sessions != 1 {
		t.Errorf("status of a full journal: %+v %v", s, err)
	}
}

// Concurrent writers (MCP over HTTP serves calls at once) keep every line
// whole.
func TestWrite_ConcurrentLinesStayWhole(t *testing.T) {
	isolate(t)
	Begin(SurfaceMCPHTTP, "t")
	if err := config.SetJournal(true); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { Write(Record{Tool: "vm_list", Args: []string{strings.Repeat("a", 200)}}) })
	}
	wg.Wait()
	if rs := records(t); len(rs) != 20 {
		t.Errorf("records %d, want 20", len(rs))
	}
}

// Export copies the journal and leaves it in place.
func TestExport(t *testing.T) {
	isolate(t)
	dst := filepath.Join(t.TempDir(), "out.jsonl")
	if _, err := Export(dst); err != ErrEmpty {
		t.Fatalf("export of no journal: %v, want ErrEmpty", err)
	}
	Begin(SurfaceCLI, "t")
	if err := config.SetJournal(true); err != nil {
		t.Fatal(err)
	}
	Write(Record{Tool: "vm_list"})
	s, err := Export(dst)
	if err != nil || s.Records != 1 {
		t.Fatalf("export: %+v %v", s, err)
	}
	a, _ := os.ReadFile(Path())
	b, _ := os.ReadFile(dst)
	if string(a) != string(b) || len(a) == 0 {
		t.Error("the export is not the journal")
	}
}
