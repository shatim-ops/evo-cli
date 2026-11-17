// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"bufio"
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

// maxLine is the longest line read: a line evo writes is a few hundred
// bytes; a longer one is not of evo's, and the rest of its file is
// skipped with it.
const maxLine = 16 << 10

// maxText is the longest name, target or id a line may carry.
const maxText = 512

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
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), maxLine)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 || j.seen[sc.Text()] {
			continue
		}
		j.seen[sc.Text()] = true
		var l Line
		if json.Unmarshal(sc.Bytes(), &l) != nil || !wellFormed(l) || (check != nil && !check(l)) {
			j.Skipped++
			continue
		}
		j.Lines = append(j.Lines, l)
	}
	if sc.Err() != nil {
		j.Skipped++
	}
}

// wellFormed tells a line of evo's form: this format, an outcome of the
// list, a time, the id of a record, texts of a bounded length on one line.
func wellFormed(l Line) bool {
	if l.Format != Format || !Known(l.Outcome) || !intentForm.MatchString(l.IntentID) || l.Tool == "" {
		return false
	}
	if _, err := time.Parse(time.RFC3339, l.Time); err != nil {
		return false
	}
	for _, s := range []string{l.Tool, l.Surface, l.ProjectID, l.Zone, l.Name, l.Target, l.Kind, l.Parent, l.ResourceID, l.Reason, l.OperationID, l.JobID, l.StateDir} {
		if len(s) > maxText || strings.ContainsAny(s, "\n\r") {
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
