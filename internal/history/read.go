// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// fileForm is the name of a file of a day: anything else in the directory
// is not read.
var fileForm = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.jsonl$`)

// intentForm is the form of the id of a record of an intent.
var intentForm = regexp.MustCompile(`^[0-9a-f]{16}$`)

// maxText is the longest text a field of a line carries: the writer cuts
// a longer one (Append), so what evo writes evo reads — an S3 key is up
// to 1024 bytes («Риск», conclusion 311, У1).
const maxText = 2048

// maxLine is the longest line read: every field at maxText still fits.
// A longer line is not of evo's; it is skipped by itself, and the lines
// after it are read.
const maxLine = 64 << 10

// Journal is what a read of the journal found: the lines in their order,
// and how many were not read — of another form, a reason or a tool a
// reader does not know, broken. A line not read does not fail the read:
// the file is the user's, and a line in it is untrusted input (the ТЗ of
// the journal, §0, 6, У5).
type Journal struct {
	Lines   []Line
	Skipped int
	// seen are the lines read, as they are: one read twice is one.
	seen map[string]bool
}

// Check is what a reader knows of a line beyond its form: the tool is of
// the registry, the reason is a code of evo. nil checks the form only.
type Check func(l Line) bool

// ReadAll reads the live lines of the files pruned (LiveName), then every
// file of a day in dir, oldest first. A file that is not a regular one of
// evo's form — a link, a directory, another name — is not read. A line
// read twice — a pruning killed between keeping a live line and removing
// its file — is one line.
func ReadAll(dir string, check Check) (Journal, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Journal{}, nil
		}
		return Journal{}, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && fileForm.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	names = append([]string{LiveName}, names...)
	j := Journal{seen: map[string]bool{}}
	for _, name := range names {
		data, err := config.ReadFileNoFollow(filepath.Join(dir, name))
		if err != nil {
			if !os.IsNotExist(err) {
				j.Skipped++
			}
			continue
		}
		j.read(data, check)
	}
	return j, nil
}

// read adds the lines of the file data.
func (j *Journal) read(data []byte, check Check) {
	for _, raw := range lines(data) {
		if j.seen[string(raw)] {
			continue
		}
		j.seen[string(raw)] = true
		var l Line
		if len(raw) > maxLine || json.Unmarshal(raw, &l) != nil || !wellFormed(l) || (check != nil && !check(l)) {
			j.Skipped++
			continue
		}
		j.Lines = append(j.Lines, l)
	}
}

// lines are the lines of data that are not blank, whatever their length:
// a line too long for a reader is skipped by itself, not the rest of its
// file with it («Риск», conclusion 311, У1).
func lines(data []byte) [][]byte {
	var out [][]byte
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		raw = bytes.TrimRight(raw, "\r")
		if len(bytes.TrimSpace(raw)) != 0 {
			out = append(out, raw)
		}
	}
	return out
}

// wellFormed tells a line of evo's form: this format, an outcome of the
// list, a time in UTC as evo writes it — the lines are ordered by it —
// the id of a record, texts of a bounded length. A text may hold a line
// break: it is a value in JSON, escaped, on the line of its record; the
// answer marks it untrusted and the table of the CLI cleans it.
func wellFormed(l Line) bool {
	if l.Format != Format || !Known(l.Outcome) || !intentForm.MatchString(l.IntentID) || l.Tool == "" {
		return false
	}
	// UTC only: a time with an offset would order the lines wrong, and a
	// line of a stranger would close a resource (311-1).
	if _, err := time.Parse(time.RFC3339, l.Time); err != nil || !strings.HasSuffix(l.Time, "Z") {
		return false
	}
	for _, s := range []string{l.Tool, l.Surface, l.ProjectID, l.Zone, l.Name, l.Target, l.Kind, l.Parent, l.ResourceID, l.Reason, l.OperationID, l.JobID, l.StateDir} {
		if len(s) > maxText {
			return false
		}
	}
	return true
}

// Since are the lines of j written at since or after.
func (j Journal) Since(since time.Time) []Line {
	var out []Line
	for _, l := range j.Lines {
		if at, _ := time.Parse(time.RFC3339, l.Time); !at.Before(since) {
			out = append(out, l)
		}
	}
	return out
}
