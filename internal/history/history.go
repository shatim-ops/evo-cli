// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package history is the journal of changes of evo (D43): a line for each
// end of a call that changes the cloud — what the call made or removed,
// and what is not known of it — kept beside the records of intents
// (internal/intent) in the same data directory. It is how evo finds again
// what it made: a record of an intent says a call is under way or cut, a
// line here says how it ended, and a later line of the same intent says
// what was found of it after.
//
// A line holds no value of an argument: the names of the resource the
// record of the intent holds (a name, «argument=id»), the ids of the
// answer, the outcome and the code of an error. A secret is not in it by
// construction, not by a mask. The file is append-only; it is not sent
// anywhere.
//
// The journal of changes is not the session journal (internal/journal,
// D46, evo diag): that one is a dogfood's record of every call, off by
// default; this one is on always (the owner's decision of 17.11) and
// holds the changes only.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The outcomes of a line: a closed list.
const (
	// Created: the call made a resource; ResourceID names it.
	Created = "created"
	// Deleted: the call removed the resource.
	Deleted = "deleted"
	// AlreadyDeleted: the resource was not there to remove (D26).
	AlreadyDeleted = "already_deleted"
	// Changed: the call changed a resource that was there and is there
	// after it — a reboot, a resize, a rule of a group.
	Changed = "changed"
	// Error: the cloud refused the call; nothing was changed. Reason is
	// its code.
	Error = "error"
	// Unknown: what the call did is not known — it was cut, the cloud's
	// answer named nothing, the operation goes on, or a person was asked
	// about it. The line is live until a later line of the same intent.
	Unknown = "unknown"
	// Found: a later call read what an earlier one of the same intent did
	// — its resource is listed, its repeat answered, its operation ended.
	Found = "found"
	// NotFound: a read of evo answered that the resource of the intent is
	// not there.
	NotFound = "not_found"
)

// outcomes is the closed list of the outcomes above.
var outcomes = map[string]bool{
	Created: true, Deleted: true, AlreadyDeleted: true, Changed: true,
	Error: true, Unknown: true, Found: true, NotFound: true,
}

// Known tells an outcome of the closed list.
func Known(outcome string) bool { return outcomes[outcome] }

// Outcome is what the end of a call says of it: the part of a line the
// caller knows and the record of the intent does not.
type Outcome struct {
	Result string
	// Kind is the kind of the resource ("disk"): with ResourceID it tells
	// one resource from another of the same id or name.
	Kind string
	// Parent is the resource the resource lives in, as «argument=id»
	// ("cluster_id=…", "bucket=…"): a name is one in one parent only.
	Parent      string
	ResourceID  string
	Reason      string
	OperationID string
	JobID       string
}

// Format is the version of the form of a line: a reader skips a line of
// another form rather than reads it wrong (a newer evo wrote it).
const Format = 1

// Line is one line of the journal.
type Line struct {
	// Format is the form of the line (Format); Append sets it.
	Format int `json:"format"`
	// Time is when the line was written, by internal/clock: a value
	// history gives in its answer (CLAUDE.md §5).
	Time      string `json:"time"`
	Tool      string `json:"tool"`
	Surface   string `json:"surface,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Zone      string `json:"zone,omitempty"`
	// Name and Target are how the record of the intent names the
	// resource: a name, «argument=id». Not a value of a secret.
	Name        string `json:"name,omitempty"`
	Target      string `json:"target,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Parent      string `json:"parent,omitempty"`
	ResourceID  string `json:"resource_id,omitempty"`
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	// IntentID is the id of the record of the intent: the lines of one
	// call share it, and the last one says what is known of it.
	IntentID string `json:"intent_id"`
	JobID    string `json:"job_id,omitempty"`
	// StateDir is the folder of the state the call went with; "" for
	// the configuration's (evo login).
	StateDir string `json:"state_dir,omitempty"`
}

// DirName is the directory of the journal: beside intents/.
const DirName = "history"

// DirOf is the directory of the journal of the intents of intentsDir: the
// journal is of the same folder of state, or of the configuration (evo
// login), as the intents are.
func DirOf(intentsDir string) string {
	return filepath.Join(filepath.Dir(intentsDir), DirName)
}

// lockWait is how long a line waits for another process that writes one:
// a line is a write of a few hundred bytes.
const lockWait = 2 * time.Second

// Append writes l as one line of the file of its day (UTC) in dir, under
// the lock of the directory. A line whose outcome is not of the list, or
// whose time is not a time, is not written. The directory and the file
// are evo's own: a link in their place is refused, not followed.
func Append(dir string, l Line) error {
	if !Known(l.Outcome) {
		return fmt.Errorf("outcome %q is not of the journal", l.Outcome)
	}
	l.Format = Format
	at, err := time.Parse(time.RFC3339, l.Time)
	if err != nil {
		return err
	}
	if fi, err := os.Lstat(dir); err == nil && !fi.IsDir() {
		return fmt.Errorf("%s is not a directory of evo", dir)
	}
	if err := config.MkdirPrivate(dir); err != nil {
		return err
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	unlock, err := state.FileLock(filepath.Join(dir, DirName+".lock"), lockWait)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := config.OpenNoFollow(FileOf(dir, at), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	// A write cut short — a kill, a full disk — leaves a line with no
	// end: the new line starts on a line of its own, so it is read whole,
	// and the torn bytes stay a line a reader skips (Астра, #2167).
	line := append(data, '\n')
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, fi.Size()-1); err != nil {
			f.Close()
			return err
		}
		if last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// FileOf is the file of the day of at in dir.
func FileOf(dir string, at time.Time) string {
	return filepath.Join(dir, at.UTC().Format("2006-01-02")+".jsonl")
}

// Live are the lines of what may still be in the cloud, in the order of
// lines: the reading history and the cleanup share (the ТЗ of the
// journal, §0, 7 and 9). A line closes only on an outcome that is certain;
// in doubt it is live («Риск», conclusion 300). Two things are live:
//
//   - a call whose last line is unknown, or found with no id: what it did
//     is not known, or not where;
//   - a resource with a line created or found and no later line that says
//     it is gone (deleted, already_deleted, not_found) of the same kind,
//     parent and id in the same project — a database of one cluster is
//     not the database of the name in another (Астра, #2165). A line with no project — a call on a
//     resource by its id, whose project the call did not know (R-82) —
//     meets a line of any project of the same kind and id: an id names one
//     resource; a name in two projects names two (У4).
//
// A line changed, error or found with no id closes nothing: a VM rebooted
// or resized is the VM created, live until it is deleted. A line of a form
// or an outcome a reader does not know is live: a newer evo wrote it, and
// what it says is not this one's to drop.
func Live(lines []Line) []Line {
	live := liveMask(lines)
	var out []Line
	for i, l := range lines {
		if live[i] {
			out = append(out, l)
		}
	}
	return out
}

// liveMask is Live by position: live[i] tells lines[i].
func liveMask(lines []Line) []bool {
	last := map[string]int{}
	type held struct {
		i       int
		project string
	}
	byResource := map[string][]held{}
	for i, l := range lines {
		// A line of a form or an outcome this reader does not know is
		// kept, as data, and acts on no other: what it says is not
		// this reader's to trust (Астра, #2168).
		if l.Format != Format || !Known(l.Outcome) {
			continue
		}
		last[l.IntentID] = i
		if l.ResourceID == "" {
			continue
		}
		key := l.Kind + "\x00" + l.Parent + "\x00" + l.ResourceID
		switch l.Outcome {
		case Created, Found:
			byResource[key] = append(byResource[key], held{i, l.ProjectID})
		case Deleted, AlreadyDeleted, NotFound:
			kept := byResource[key][:0]
			for _, h := range byResource[key] {
				if h.project != "" && l.ProjectID != "" && h.project != l.ProjectID {
					kept = append(kept, h)
				}
			}
			byResource[key] = kept
		}
	}
	live := map[int]bool{}
	for _, i := range last {
		if l := lines[i]; l.Outcome == Unknown || (l.Outcome == Found && l.ResourceID == "") {
			live[i] = true
		}
	}
	for i, l := range lines {
		if l.Format != Format || !Known(l.Outcome) {
			live[i] = true
		}
	}
	for _, hs := range byResource {
		for _, h := range hs {
			live[h.i] = true
		}
	}
	mask := make([]bool, len(lines))
	for i := range lines {
		mask[i] = live[i]
	}
	return mask
}
