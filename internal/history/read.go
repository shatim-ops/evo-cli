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
	"unicode/utf8"

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
}

// Check is what a reader knows of a line beyond its form: the tool is of
// the registry, the reason is a code of evo. nil checks the form only.
type Check func(l Line) bool

// ReadAll reads every file of a day in dir, oldest first. A file that is
// not a regular one of evo's form — a link, a directory, another name —
// is not read.
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
	var j Journal
	for _, name := range names {
		data, err := config.ReadFileNoFollow(filepath.Join(dir, name))
		if err != nil {
			j.Skipped++
			continue
		}
		j.read(data, check)
	}
	return j, nil
}

// read adds the lines of the file data.
func (j *Journal) read(data []byte, check Check) {
	for _, raw := range lines(data) {
		l, ok := parse(raw)
		if !ok || (check != nil && !check(l)) {
			j.Skipped++
			continue
		}
		j.Lines = append(j.Lines, l)
	}
}

// parse is the line raw as a reader takes it, and whether it is one of
// evo's form: not longer than a reader takes, valid UTF-8 — evo writes it
// by the JSON encoder, which never writes another; decoded, a byte that is
// not would become U+FFFD, and two resources of different ids one, a
// broken line closing another's live one (Астра, #2260) — and wellFormed.
func parse(raw []byte) (Line, bool) {
	var l Line
	if len(raw) > maxLine || !utf8.Valid(raw) || loneSurrogate(raw) || json.Unmarshal(raw, &l) != nil || !wellFormed(l) {
		return Line{}, false
	}
	return l, true
}

// loneSurrogate tells a line that escapes half of a surrogate pair alone
// (\ud800): ASCII on the line, but the decoder makes it U+FFFD, as it makes
// a byte that is not UTF-8 — two ids would become one («Риск», conclusion
// 331, У1). The JSON encoder of evo never writes one. A pair (\ud83d\ude00)
// is a character, and an escaped backslash is skipped whole.
func loneSurrogate(raw []byte) bool {
	surrogate := func(i int) (rune, bool) {
		if i+6 > len(raw) || raw[i] != '\\' || raw[i+1] != 'u' {
			return 0, false
		}
		var r rune
		for _, c := range raw[i+2 : i+6] {
			switch {
			case c >= '0' && c <= '9':
				r = r<<4 | rune(c-'0')
			case c >= 'a' && c <= 'f':
				r = r<<4 | rune(c-'a'+10)
			case c >= 'A' && c <= 'F':
				r = r<<4 | rune(c-'A'+10)
			default:
				return 0, false
			}
		}
		return r, true
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		r, ok := surrogate(i)
		switch {
		case !ok:
			i++ // \\, \", \n…: the next byte is the escape's own
		case r >= 0xD800 && r <= 0xDBFF:
			if low, ok := surrogate(i + 6); !ok || low < 0xDC00 || low > 0xDFFF {
				return true
			}
			i += 11
		case r >= 0xDC00 && r <= 0xDFFF:
			return true
		default:
			i += 5
		}
	}
	return false
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
