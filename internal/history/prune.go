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
	"sort"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The bounds of the journal (the owner's decision of 17.11: 90 days,
// 64 MB; the ТЗ of the journal, §0, 9).
const (
	// KeepDays is how long a file of a day stays.
	KeepDays = 90
	// Ceiling is the size the journal is brought under, closed lines
	// first.
	Ceiling = 64 << 20
)

// ceiling is Ceiling; tests lower it.
var ceiling int64 = Ceiling

const (
	// LiveName is the file the live lines of the files retired go to:
	// read always, whatever their age.
	LiveName = "live.jsonl"
	// prunedName marks the day of the last pruning: once a day.
	prunedName = "pruned"
	// pruneLimit bounds a pruning: what does not fit goes on next time.
	pruneLimit = 2 * time.Second
)

// Pruned is what a pruning did and what the journal is after it.
type Pruned struct {
	// Retired are the files of days removed, their live lines kept.
	Retired []string
	// Live is how many live lines the journal holds.
	Live int
	// OverCeiling: the live lines alone are more than the ceiling — none
	// is removed; doctor and history say so.
	OverCeiling bool
}

// rawLine is a line of a file as it is, and what a reader makes of it.
type rawLine struct {
	text string
	line Line
	ok   bool
}

// Prune keeps the journal of dir in its bounds: a file of a day older than
// KeepDays goes, then the oldest ones while the journal is over the
// ceiling — today's never. What is live in a file that goes is kept in
// LiveName first: a resource made and not removed, an outcome not known,
// and any line this reader cannot read or does not know (a newer evo wrote
// it: not this one's to drop). The live lines are written whole to a temp
// file and renamed, then the files go: a kill between the two leaves a
// line twice, which a reader reads once (ReadAll), never none.
//
// It runs at most once a day (a mark in the directory), under the lock of
// the journal and a limit of time: what does not fit goes on next time. A
// directory that is not there is nothing to prune.
func Prune(dir string, now time.Time) (Pruned, error) {
	var p Pruned
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return p, nil
	}
	unlock, err := state.FileLock(filepath.Join(dir, DirName+".lock"), lockWait)
	if err != nil {
		return p, err
	}
	defer unlock()
	today := now.UTC().Format(time.DateOnly)
	if mark, err := config.ReadFileNoFollow(filepath.Join(dir, prunedName)); err == nil && strings.TrimSpace(string(mark)) == today {
		return p, nil
	}
	deadline := time.Now().Add(pruneLimit) // wall-clock: the limit of one pruning, not a value of an answer

	days, sizes, err := dayFiles(dir)
	if err != nil {
		return p, err
	}
	var all []rawLine
	from := map[string][2]int{} // file → its lines in all
	for _, name := range append([]string{LiveName}, days...) {
		data, err := config.ReadFileNoFollow(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return p, err
		}
		start := len(all)
		all = append(all, rawLines(data)...)
		from[name] = [2]int{start, len(all)}
		if time.Now().After(deadline) { // wall-clock: the limit of one pruning
			return p, nil
		}
	}
	live := liveOfRaw(all)

	// The files that go: too old, then the oldest while over the ceiling.
	cutoff := now.UTC().AddDate(0, 0, -KeepDays).Format(time.DateOnly)
	var total int64
	for _, n := range days {
		total += sizes[n]
	}
	if fi, err := os.Lstat(filepath.Join(dir, LiveName)); err == nil {
		total += fi.Size()
	}
	// A file that goes frees the bytes of its closed lines: its live
	// lines move to LiveName. Over the ceiling only a file with closed
	// lines goes — one of live lines alone frees nothing.
	closed := func(name string) (n int64) {
		r := from[name]
		for i := r[0]; i < r[1]; i++ {
			if !live[i] {
				n += int64(len(all[i].text) + 1)
			}
		}
		return n
	}
	retire := map[string]bool{}
	for _, n := range days {
		day := strings.TrimSuffix(n, ".jsonl")
		if free := closed(n); day < cutoff || (total > ceiling && day != today && free > 0) {
			retire[n] = true
			total -= free
		}
	}

	// The live lines of LiveName and of the files that go, once each.
	var kept bytes.Buffer
	seen := map[string]bool{}
	for _, name := range append([]string{LiveName}, days...) {
		r, ok := from[name]
		if !ok || (name != LiveName && !retire[name]) {
			continue
		}
		for i := r[0]; i < r[1]; i++ {
			if live[i] && !seen[all[i].text] {
				seen[all[i].text] = true
				kept.WriteString(all[i].text)
				kept.WriteByte('\n')
			}
		}
	}
	var liveBytes int64
	counted := map[string]bool{}
	for i := range all {
		if live[i] && !counted[all[i].text] {
			counted[all[i].text] = true
			p.Live++
			liveBytes += int64(len(all[i].text) + 1)
		}
	}
	p.OverCeiling = liveBytes > ceiling

	if len(retire) > 0 || from[LiveName] != [2]int{} {
		if err := config.WriteFileAtomic(filepath.Join(dir, LiveName), kept.Bytes(), 0o600); err != nil {
			return p, err
		}
	}
	for _, n := range days {
		if !retire[n] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !os.IsNotExist(err) {
			return p, err
		}
		p.Retired = append(p.Retired, n)
	}
	return p, config.WriteFileAtomic(filepath.Join(dir, prunedName), []byte(today+"\n"), 0o600)
}

// dayFiles are the files of days of dir, oldest first, with their sizes:
// regular files of evo's name only.
func dayFiles(dir string) ([]string, map[string]int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	sizes := map[string]int64{}
	var names []string
	for _, e := range entries {
		if !e.Type().IsRegular() || !fileForm.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		names = append(names, e.Name())
		sizes[e.Name()] = info.Size()
	}
	sort.Strings(names)
	return names, sizes, nil
}

// rawLines are the lines of data as they are, each with what a reader
// makes of it; an empty line is none.
func rawLines(data []byte) []rawLine {
	var out []rawLine
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), maxLine)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		r := rawLine{text: sc.Text()}
		r.ok = json.Unmarshal(sc.Bytes(), &r.line) == nil && wellFormed(r.line)
		out = append(out, r)
	}
	return out
}

// liveOfRaw is the live mask of raw lines: Live of the lines a reader
// reads, and every line it does not — kept, never dropped.
func liveOfRaw(all []rawLine) []bool {
	var lines []Line
	var at []int
	for i, r := range all {
		if r.ok {
			lines = append(lines, r.line)
			at = append(at, i)
		}
	}
	mask := liveMask(lines)
	live := make([]bool, len(all))
	for i, r := range all {
		live[i] = !r.ok
	}
	for k, i := range at {
		live[i] = live[i] || mask[k]
	}
	return live
}
