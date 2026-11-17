// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func line(outcome string) Line {
	return Line{Time: "2026-11-17T12:00:00Z", Tool: "pg_create", Name: "db", ResourceID: "c-1", Outcome: outcome, IntentID: "0123456789abcdef"}
}

func readLines(t *testing.T, path string) []Line {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Line
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("not a line of JSON: %q", sc.Text())
		}
		out = append(out, l)
	}
	return out
}

// A line goes to the file of its day, appended, private to the user.
func TestAppend_OneLineToTheFileOfItsDay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	if err := Append(dir, line(Unknown)); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, line(Found)); err != nil {
		t.Fatal(err)
	}
	path := FileOf(dir, time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
	got := readLines(t, path)
	if len(got) != 2 || got[0].Outcome != Unknown || got[1].Outcome != Found || got[1].IntentID != "0123456789abcdef" || got[0].Format != Format {
		t.Fatalf("lines %+v; want unknown, then found, of the same intent", got)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v, want 0600", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
			t.Errorf("directory mode %v, want 0700", fi.Mode().Perm())
		}
	}
}

// An outcome outside the list, or a line with no time, is not written: a
// reader takes the outcome from a closed list.
func TestAppend_OutcomeOfTheListOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	if err := Append(dir, line("made")); err == nil {
		t.Error("an outcome outside the list was written")
	}
	l := line(Created)
	l.Time = ""
	if err := Append(dir, l); err == nil {
		t.Error("a line with no time was written")
	}
}

// The directory and the file are evo's own: a link in the place of
// either — committed to a repository, it comes with git clone — is
// refused, and nothing is written where it points.
func TestAppend_ALinkIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege on Windows")
	}
	root := t.TempDir()
	elsewhere := filepath.Join(root, "elsewhere")
	if err := os.Mkdir(elsewhere, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedDir := filepath.Join(root, "a", DirName)
	_ = os.MkdirAll(filepath.Dir(linkedDir), 0o700)
	if err := os.Symlink(elsewhere, linkedDir); err != nil {
		t.Fatal(err)
	}
	if err := Append(linkedDir, line(Created)); err == nil {
		t.Error("a line was written through a link in the place of the directory")
	}
	dir := filepath.Join(root, "b", DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(elsewhere, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, FileOf(dir, time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, line(Created)); err == nil {
		t.Error("a line was written through a link in the place of the file")
	}
	if b, _ := os.ReadFile(target); len(b) != 0 {
		t.Errorf("the target of the link got %q", b)
	}
}

// What is live: a resource created and not removed — a change of it does
// not close it («Риск», the condition on the code of D43) — and a call
// whose last line is unknown; a line of another form is kept.
func TestLive_AChangeDoesNotCloseACreate(t *testing.T) {
	l := func(intent, outcome, resource string) Line {
		return Line{Format: Format, Time: "2026-11-17T12:00:00Z", Tool: "x", IntentID: intent, Outcome: outcome, ResourceID: resource}
	}
	ids := func(lines []Line) (out []string) {
		for _, x := range lines {
			out = append(out, x.IntentID+":"+x.Outcome+":"+x.ResourceID)
		}
		return out
	}
	lines := []Line{
		l("a", Created, "vm-1"),
		l("b", Changed, "vm-1"),                      // a reboot of it
		l("c", Changed, ""),                          // a resize, no id
		l("d", Created, "vm-2"),                      //
		l("e", Deleted, "vm-2"),                      // removed
		l("f", Unknown, ""),                          // cut, named by name only
		l("g", Unknown, ""),                          // cut ...
		l("g", Found, "disk-1"),                      // ... and found later
		l("h", Error, ""),                            // refused
		l("i", Created, "vm-3"),                      //
		l("j", AlreadyDeleted, "vm-3"),               // gone before its delete
		l("k", Created, "vm-4"),                      //
		l("l", NotFound, "vm-4"),                     // a read found it gone
		{Format: 2, IntentID: "m", Outcome: "moved"}, // a newer evo
	}
	got := ids(Live(lines))
	want := []string{"a:created:vm-1", "f:unknown:", "g:found:disk-1", "m:moved:"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("live %v, want %v", got, want)
	}
}
