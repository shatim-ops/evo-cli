// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/journal"
)

// journalMode is the state of the session journal a call runs in.
type journalMode string

const (
	journalOff    journalMode = "off"
	journalOnMode journalMode = "on"
	// journalBroken: on, and its file cannot be written — a directory
	// stands where the file goes.
	journalBroken journalMode = "broken"
)

// setJournal puts the journal of a fresh configuration directory into mode
// and begins a session.
func setJournal(t *testing.T, mode journalMode) {
	t.Helper()
	t.Setenv(config.EnvConfigDir, filepath.Join(t.TempDir(), "evo"))
	if err := config.SetJournal(mode != journalOff); err != nil {
		t.Fatal(err)
	}
	if mode == journalBroken {
		if err := os.MkdirAll(journal.Path(), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	journal.Begin(journal.SurfaceCLI, "test")
}

// answersIn calls every tool of the contract world once with its contract
// arguments: "tool" → the answer and whether it is an error.
func answersIn(t *testing.T, mode journalMode) map[string]string {
	t.Helper()
	setJournal(t, mode)
	rt := &contractRT{kind: "200"}
	reg := contractReg(t, rt)
	out := map[string]string{}
	for _, tl := range reg.Tools() {
		if contractHTTPSkip[tl.Name] {
			continue
		}
		text, isErr, pan := contractCall(tl, contractArgs(tl), contractCallLimit)
		rt.take()
		if pan != "" {
			continue
		}
		if isErr {
			text = "isError " + text
		}
		out[tl.Name] = text
	}
	return out
}

// D46: the journal records the call and never changes it. Every tool
// answers the same, byte for byte, with the journal off, on, and on with a
// file that cannot be written — a lost record is the journal's loss, not
// the call's.
func TestJournal_AnswerSameOnOffAndBroken(t *testing.T) {
	off := answersIn(t, journalOff)
	if len(off) == 0 {
		t.Fatal("no tool answered")
	}
	for _, mode := range []journalMode{journalOnMode, journalBroken} {
		got := answersIn(t, mode)
		names := make([]string, 0, len(off))
		for n := range off {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if journalVolatile(got[n]) != journalVolatile(off[n]) {
				t.Errorf("%s: the answer with the journal %s is not the answer with it off:\n  off: %s\n  %s: %s", n, mode, clip(off[n]), mode, clip(got[n]))
			}
		}
	}
}

// A journal that is off leaves nothing: no file, no lock — a call of every
// tool in a session that began.
func TestJournal_OffLeavesNoTrace(t *testing.T) {
	answersIn(t, journalOff)
	for _, name := range []string{"journal.jsonl", "journal.lock"} {
		if _, err := os.Lstat(filepath.Join(filepath.Dir(journal.Path()), name)); err == nil {
			t.Errorf("the journal is off, yet %s is there", name)
		}
	}
}

// D46: a record is metadata. With the journal on, every tool is called
// with its contract arguments: no value of an argument and nothing of the
// cloud's answer (the id and the name it gives) is in the file, and every
// record names the version of evo and its session (owner's decision
// 27.10: one file for the whole dogfood, each record with both).
func TestJournal_NoValueOfAnyTool(t *testing.T) {
	answersIn(t, journalOnMode)
	data, err := os.ReadFile(journal.Path())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	reg := New(nil)
	names := map[string]bool{}
	values := map[string]string{contractID: "the cloud's id"}
	for _, tl := range reg.Tools() {
		names[tl.Name] = true
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for k := range props {
			names[k] = true
		}
		for k, v := range contractArgs(tl) {
			if s, ok := v.(string); ok && len(s) >= 4 {
				values[s] = tl.Name + "." + k
			}
		}
	}
	for v, from := range values {
		if names[v] {
			continue // a name of a tool or an argument, which the journal may hold
		}
		if strings.Contains(text, v) {
			t.Errorf("a value (%s) is in the journal", from)
		}
	}
	rs := journalRecords(t)
	if len(rs) == 0 {
		t.Fatal("no record")
	}
	for _, r := range rs {
		if r.Evo == "" || r.Session == "" || r.Surface == "" {
			t.Errorf("a record without the version, the session or the surface: %+v", r)
			break
		}
	}
}

// journalVolatileRE: what each call makes anew — the id of a stack job or
// a plan, a time.
var journalVolatileRE = regexp.MustCompile(`stk-[0-9a-f]{10}|plan-[0-9a-f]{12}|\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`)

func journalVolatile(s string) string { return journalVolatileRE.ReplaceAllString(s, "<new>") }

func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
