// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"os"
	"testing"
	"time"
)

// A record of a call killed before it sent anything says so itself: its
// writes were counted (Counts) and the first was not marked (Sent). No
// reader keeps it — the request did not go out. Every other record stays,
// whatever its age: one whose first write was marked, one of a call in
// flight, one that ended or is held, one of evo before the mark, one of
// another machine that shares the directory («Риск», conclusion 187, У1,
// У2, У2-3 and У6).
func TestPending_ARecordOfAKilledCallThatSentNothingIsNotKept(t *testing.T) {
	dir := t.TempDir()
	gone := deadPID(t)
	long := time.Now().Add(-90 * 24 * time.Hour)
	open := func(in Intent) Intent {
		in.Tool, in.Target, in.StartedAt = "vm_reboot", "vm_id=x", long
		got, err := Open(dir, in)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	unsent := open(Intent{PID: gone, Counts: true})
	kept := map[string]string{
		open(Intent{PID: gone, Counts: true, Sent: true}).ID:                  "its first write was marked",
		open(Intent{PID: os.Getpid(), Counts: true}).ID:                       "its call is in flight",
		open(Intent{PID: gone, Counts: true, Held: true}).ID:                  "its operation goes on",
		open(Intent{PID: gone}).ID:                                            "of evo before the mark",
		open(Intent{PID: gone, Note: "evo login: the key"}).ID:                "evo login's own",
		open(Intent{PID: gone, Counts: true, Machine: "0123456789abcdef"}).ID: "it was made on another machine, whose pid this one does not tell",
	}
	got := map[string]bool{}
	for _, in := range Pending(dir) {
		got[in.ID] = true
	}
	if got[unsent.ID] {
		t.Error("a record of a killed call that sent nothing was kept")
	}
	if _, err := os.Stat(recordPath(dir, unsent.ID)); !os.IsNotExist(err) {
		t.Errorf("its file stays: %v", err)
	}
	for id, why := range kept {
		if !got[id] {
			t.Errorf("a record dropped though %s", why)
		}
	}
}

// The mark is written before the write goes out, and a mark that cannot
// be written says so: the write is not sent.
func TestMarkSent_WrittenOrRefused(t *testing.T) {
	dir := t.TempDir()
	in, err := Open(dir, Intent{Tool: "vm_reboot", Target: "vm_id=x", PID: os.Getpid(), Counts: true, StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkSent(in); err != nil {
		t.Fatal(err)
	}
	if got := Pending(dir); len(got) != 1 || !got[0].Sent {
		t.Fatalf("the mark is not in the file: %+v", got)
	}
	in.ID = "not-an-id"
	if err := MarkSent(in); err == nil {
		t.Error("a mark that could not be written was taken for written")
	}
}
