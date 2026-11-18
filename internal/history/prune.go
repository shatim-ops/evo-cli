// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
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

// killed says whether a pruning is killed after the write it has just made:
// never; tests kill it there, as a kill of evo would.
var killed = func() bool { return false }

// planned is called when a pruning has read the journal and checked its
// states, before it takes the lock to write: tests write a line there.
var planned = func() {}

const (
	// LiveName is the file the live lines of the files retired go to:
	// read always, whatever their age.
	LiveName = "live.jsonl"
	// StrayName is the file the lines a pruning found in a file not their
	// day's go to (ofFile): no reader takes them, none is dropped («Риск»,
	// 324-3; #2297: what evo does not understand is kept).
	StrayName = "stray.jsonl"
	// prunedName marks the day of the last pruning: once a day.
	prunedName = "pruned"
	// retiredName names the newest day whose file was retired: the
	// lines of that day and before are kept only if live.
	retiredName = "retired"
	// deferredName names, one a line, the files that hold lines evo does
	// not write where a pruning was left undone (Pruned.Deferred): doctor
	// says so; the next pruning that goes on removes it.
	deferredName = "deferred"
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
	// Deferred are the files whose lines evo does not write — a line of
	// another day, a line in LiveName not live — when the pruning was left
	// undone: going on would change what the reader finds live. Doctor
	// names them; the pruning tries again the next day.
	Deferred []string
}

// rawLine is a line of a file as it is, and what a reader makes of it.
type rawLine struct {
	text string
	line Line
	ok   bool
	// stray: a line read whole that does not stand in its file (ofFile).
	// It is not taken and not live: it goes to StrayName, never to
	// LiveName, where its day is no more known.
	stray bool
}

// Prune keeps the journal of dir in its bounds: a file of a day older than
// KeepDays goes, then the oldest ones while the journal is over the
// ceiling — today's never. What is live in a file that goes is kept in
// LiveName first: a resource made and not removed, an outcome not known,
// and any line this reader cannot read or does not know (a newer evo wrote
// it: not this one's to drop) — checked by check as the reader checks it
// (ReadAll), so a line history skips closes nothing here either (Астра,
// #2297). A line of another day in the file of a day (ofFile) is kept in
// StrayName, which no reader takes lines of. The live lines are written whole to a temp
// file and renamed, then the files go: a kill between the two leaves a
// line twice, which a reader reads once (ReadAll), never none.
//
// It runs at most once a day (a mark in the directory) and within a limit
// of time: what does not fit goes on next time. It reads the journal and
// checks every state a kill can leave without the lock of the journal, and
// takes it for its writes only — a line written meanwhile waits for no
// pruning, and puts the writes off to the next call (changed). A directory
// that is not there is nothing to prune.
func Prune(dir string, now time.Time, check Check) (Pruned, error) {
	var p Pruned
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return p, nil
	}
	today := now.UTC().Format(time.DateOnly)
	prunedToday := func() bool {
		mark, err := config.ReadFileNoFollow(filepath.Join(dir, prunedName))
		return err == nil && strings.TrimSpace(string(mark)) == today
	}
	if prunedToday() {
		return p, nil
	}
	deadline := time.Now().Add(pruneLimit) // wall-clock: the limit of one pruning, not a value of an answer

	// The journal is read and every state checked without its lock: a line
	// written meanwhile waits for no pruning (lockWait). The lock is taken
	// for the writes only, and they are made only if nothing the pruning
	// read has changed since (changed); else it goes on the next time.
	days, sizes, err := dayFiles(dir)
	if err != nil {
		return p, err
	}
	retired := RetiredThrough(dir)
	read := map[string]os.FileInfo{} // the files read, as they were
	var all []rawLine
	from := map[string][2]int{} // file → its lines in all
	for _, name := range readOrder(days) {
		if fi, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			read[name] = fi
		}
		data, err := config.ReadFileNoFollow(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return p, err
		}
		start := len(all)
		all = append(all, rawLines(name, retired, data, check)...)
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
	for _, n := range []string{LiveName, StrayName} {
		if fi, err := os.Lstat(filepath.Join(dir, n)); err == nil {
			total += fi.Size()
		}
	}
	// A file that goes frees the bytes of its closed lines: its live
	// lines move to LiveName, its stray ones to StrayName. Over the ceiling only a file with closed
	// lines goes — one of live lines alone frees nothing.
	closed := func(name string) (n int64) {
		r := from[name]
		for i := r[0]; i < r[1]; i++ {
			if !live[i] && !all[i].stray {
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
	for _, name := range readOrder(days) {
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
	// The stray lines of StrayName, of the files that go and of LiveName,
	// which is written anew, once each.
	var strays bytes.Buffer
	straySeen := map[string]bool{}
	for i, r := from[StrayName][0], from[StrayName]; i < r[1]; i++ {
		straySeen[all[i].text] = true
		strays.WriteString(all[i].text)
		strays.WriteByte('\n')
	}
	newStrays := false
	for _, name := range append(append([]string(nil), days...), LiveName) {
		if name != LiveName && !retire[name] {
			continue
		}
		for i, r := from[name][0], from[name]; i < r[1]; i++ {
			if all[i].stray && !straySeen[all[i].text] {
				straySeen[all[i].text] = true
				newStrays = true
				strays.WriteString(all[i].text)
				strays.WriteByte('\n')
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

	through := retired
	for _, n := range days {
		if day := strings.TrimSuffix(n, ".jsonl"); retire[n] && day > through {
			through = day
		}
	}

	// What the reader finds live is what it found before, in every state
	// the pruning leaves the journal in — a kill after any of its writes,
	// and its end: each is read before the first write. A journal it would
	// change — lines evo does not write, where a line it keeps is closed by
	// one it drops (Астра, #2346, #2347) — is left as it is: the pruning
	// keeps what it cannot show it keeps.
	newLive, newStray := kept.Bytes(), strays.Bytes()
	leftIn := func(gone int, live []rawLine) []rawLine {
		var s []rawLine
		for _, n := range days {
			if r := from[n]; !retire[n] || gone == 0 {
				s = append(s, all[r[0]:r[1]]...)
			} else {
				gone--
			}
		}
		s = append(s, live...)
		return append(s, rawLines(StrayName, through, newStray, check)...)
	}
	oldLive := all[from[LiveName][0]:from[LiveName][1]]
	states := [][]rawLine{
		leftIn(0, oldLive), // the stray lines written
		leftIn(0, rawLines(LiveName, retired, newLive, check)), // LiveName
	}
	for gone := 0; gone <= len(retire); gone++ { // the mark, then each file
		states = append(states, leftIn(gone, rawLines(LiveName, through, newLive, check)))
	}
	deferred := false
	for _, st := range states {
		if deferred = !sameLive(all, live, st, liveOfRaw(st)); deferred {
			break
		}
	}

	planned()
	unlock, err := state.FileLock(filepath.Join(dir, DirName+".lock"), lockWait)
	if err != nil {
		return p, err
	}
	defer unlock()
	if prunedToday() || changed(dir, days, retired, read) {
		return Pruned{}, nil
	}
	if deferred {
		p.Retired, p.Live = nil, 0
		for _, name := range readOrder(days) {
			r := from[name]
			for i := r[0]; i < r[1]; i++ {
				if all[i].stray || name == LiveName && all[i].ok && !live[i] {
					p.Deferred = append(p.Deferred, name)
					break
				}
			}
		}
		if err := config.WriteFileAtomic(filepath.Join(dir, deferredName), []byte(strings.Join(p.Deferred, "\n")+"\n"), 0o600); err != nil {
			return p, err
		}
		return p, config.WriteFileAtomic(filepath.Join(dir, prunedName), []byte(today+"\n"), 0o600)
	}

	// The order a kill leaves the journal read as before in: the stray
	// lines first — a line kept apart and where it stood is taken in
	// neither; then LiveName — a line of a day after the mark is taken in
	// the file of its day, there still; then the mark — the lines of
	// LiveName before it are its own; then the files, oldest first.
	if newStrays {
		if err := config.WriteFileAtomic(filepath.Join(dir, StrayName), strays.Bytes(), 0o600); err != nil {
			return p, err
		}
		if killed() {
			return p, nil
		}
	}
	if len(retire) > 0 || from[LiveName] != [2]int{} {
		if err := config.WriteFileAtomic(filepath.Join(dir, LiveName), kept.Bytes(), 0o600); err != nil {
			return p, err
		}
		if killed() {
			return p, nil
		}
	}
	if through != retired {
		if err := config.WriteFileAtomic(filepath.Join(dir, retiredName), []byte(through+"\n"), 0o600); err != nil {
			return p, err
		}
		if killed() {
			return p, nil
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
		if killed() {
			return p, nil
		}
	}
	if err := os.Remove(filepath.Join(dir, deferredName)); err != nil && !os.IsNotExist(err) {
		return p, err
	}
	return p, config.WriteFileAtomic(filepath.Join(dir, prunedName), []byte(today+"\n"), 0o600)
}

// changed says whether the journal of dir is not what a pruning read: its
// files of days, the files it read — the same file, size and time — or the
// day of its mark. Lines written meanwhile are a change too: the pruning
// goes on the next time.
func changed(dir string, days []string, retired string, read map[string]os.FileInfo) bool {
	now, _, err := dayFiles(dir)
	if err != nil || !slices.Equal(now, days) || RetiredThrough(dir) != retired {
		return true
	}
	for _, name := range readOrder(days) {
		fi, err := os.Lstat(filepath.Join(dir, name))
		was, ok := read[name]
		if (err == nil) != ok || ok && (!os.SameFile(fi, was) || fi.Size() != was.Size() || !fi.ModTime().Equal(was.ModTime())) {
			return true
		}
	}
	return false
}

// DeferredFiles are the files a pruning left undone named (Pruned.Deferred):
// names of the journal's own form only, never what a file holds.
func DeferredFiles(dir string) []string {
	data, err := config.ReadFileNoFollow(filepath.Join(dir, deferredName))
	if err != nil {
		return nil
	}
	var names []string
	for _, n := range strings.Split(string(data), "\n") {
		if fileForm.MatchString(n) || n == LiveName || n == StrayName {
			names = append(names, n)
		}
	}
	return names
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

// rawLines are the lines of the file name, data, of a journal retired
// through the day retired, each with what a reader
// makes of it; an empty line is none. A line too long for a reader is one
// line it does not read — kept — and the lines after it are read as
// ReadAll reads them (lines).
func rawLines(name, retired string, data []byte, check Check) []rawLine {
	var out []rawLine
	for _, raw := range lines(data) {
		r := rawLine{text: string(raw)}
		// One reading of a line for the reader and the pruning (parse,
		// fileCheck): what history skips, the pruning keeps as it is — a
		// line of another day in StrayName.
		r.line, r.ok = parse(raw)
		r.stray = r.ok && !ofFile(name, retired, r.line)
		r.ok = r.ok && fileCheck(name, retired, check)(r.line)
		out = append(out, r)
	}
	return out
}

// liveOfRaw is the live mask of raw lines: Live of the lines a reader
// reads, and every line it does not — kept, never dropped — but a stray one,
// kept in StrayName.
func liveOfRaw(all []rawLine) []bool {
	// Each line once, where the reader first meets it and takes it
	// (readOrder, taken): a copy of a line is the line, and is as live as
	// it is; a copy the reader does not take shadows none.
	var lines []Line
	first := map[string]int{} // text → index in lines
	for _, r := range all {
		if _, dup := first[r.text]; r.ok && !dup {
			first[r.text] = len(lines)
			lines = append(lines, r.line)
		}
	}
	mask := liveMask(lines)
	live := make([]bool, len(all))
	for i, r := range all {
		live[i] = (!r.ok && !r.stray) || (r.ok && mask[first[r.text]])
	}
	return live
}

// sameLive says whether two readings of a journal take the same live
// lines: its raw lines and their live mask, before and after.
func sameLive(before []rawLine, liveBefore []bool, after []rawLine, liveAfter []bool) bool {
	taken := func(all []rawLine, live []bool) map[string]bool {
		m := map[string]bool{}
		for i, r := range all {
			if r.ok && live[i] {
				m[r.text] = true
			}
		}
		return m
	}
	b, a := taken(before, liveBefore), taken(after, liveAfter)
	if len(a) != len(b) {
		return false
	}
	for t := range b {
		if !a[t] {
			return false
		}
	}
	return true
}

// RetiredThrough is the newest day whose file a pruning retired
// (2026-08-18): the lines of that day and before are kept only if live.
// "" when none was.
func RetiredThrough(dir string) string {
	data, err := config.ReadFileNoFollow(filepath.Join(dir, retiredName))
	if err != nil {
		return ""
	}
	day := strings.TrimSpace(string(data))
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return ""
	}
	return day
}

// Summary is what doctor says of a journal: its size, its oldest day, how
// many lines are live, and whether the live ones alone are over the
// ceiling.
type Summary struct {
	Bytes       int64
	Oldest      string
	Live        int
	OverCeiling bool
	// Deferred: the files the last pruning was left undone for
	// (Pruned.Deferred).
	Deferred []string
}

// Describe reads the journal of dir for doctor, its lines checked by check
// as Prune checks them; a journal that is not there is an empty one.
func Describe(dir string, check Check) (Summary, error) {
	var s Summary
	days, sizes, err := dayFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	for _, n := range days {
		s.Bytes += sizes[n]
	}
	if len(days) > 0 {
		s.Oldest = strings.TrimSuffix(days[0], ".jsonl")
	}
	retired := RetiredThrough(dir)
	var all []rawLine
	for _, name := range readOrder(days) {
		data, err := config.ReadFileNoFollow(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if name == LiveName || name == StrayName {
			s.Bytes += int64(len(data))
		}
		all = append(all, rawLines(name, retired, data, check)...)
	}
	var liveBytes int64
	seen := map[string]bool{}
	for i, live := range liveOfRaw(all) {
		if live && !seen[all[i].text] {
			seen[all[i].text] = true
			s.Live++
			liveBytes += int64(len(all[i].text) + 1)
		}
	}
	s.OverCeiling = liveBytes > ceiling
	s.Deferred = DeferredFiles(dir)
	return s, nil
}
