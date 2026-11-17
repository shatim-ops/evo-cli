// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	stderrors "errors"
	"path/filepath"
	"sort"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The bounds of an answer of history (the ТЗ of the journal, §0, 6): the
// lines of the last historyDays days, and of them the last historyMax.
const (
	historyDays = 30
	historyMax  = 500
)

// historyLine is a line of the journal as history answers it: no folder
// of state — a local path with the user's name, not given over MCP (У6)
// and so not in the JSON of the CLI either; check names the read that
// tells an outcome not known.
type historyLine struct {
	Time        string `json:"time"`
	Tool        string `json:"tool"`
	Surface     string `json:"surface,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	Zone        string `json:"zone,omitempty"`
	Name        string `json:"name,omitempty"`
	Target      string `json:"target,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Parent      string `json:"parent,omitempty"`
	ResourceID  string `json:"resource_id,omitempty"`
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	IntentID    string `json:"intent_id"`
	JobID       string `json:"job_id,omitempty"`
	Check       string `json:"check,omitempty"`
	// CheckResult is what history with check found of a live line it did
	// not close (checkThere and the rest).
	CheckResult string `json:"check_result,omitempty"`
}

// handleHistory reads the journal of changes of the folder of state (D43):
// the lines of the window — since, or the last historyDays days, at most
// historyMax — and what is live: a resource made and not removed, and a
// call whose outcome is not known, with the read that tells it. The
// records of intents with no line (a process killed before it wrote one)
// are live too. A line the reader does not take is counted, not answered.
// On a server bound to projects (--allow-project) a line of another
// project is not answered, and a line with no project — a call on a
// resource by id, whose project the call did not know (R-82) — neither:
// the number of their calls is.
func (s *Registry) handleHistory(ctx context.Context, args map[string]any) (string, bool) {
	// The journal is read where the records of intents are written: the
	// declared directory of state (one point of decision).
	root, ok := s.stateDir()
	if !ok {
		return failed(ctx, clierrors.New(clierrors.CodeConfigInvalid,
			i18n.T("no directory of state is declared: there is no journal to read", "каталог состояния не задан: журнала для чтения нет"), ""), nil), true
	}
	since := clock.Now().AddDate(0, 0, -historyDays)
	var err error
	if v := stringArg(args, "since"); v != "" {
		if since, err = time.Parse(time.RFC3339, v); err != nil {
			if since, err = time.Parse(time.DateOnly, v); err != nil {
				return failed(ctx, clierrors.New(clierrors.CodeUsage,
					i18n.Tf("since %q is not a time: give a date (2026-11-01) or a time (2026-11-01T10:00:00Z)",
						"since %q — не время: укажите дату (2026-11-01) или время (2026-11-01T10:00:00Z)", v), ""), nil), true
			}
		}
	}
	project := stringArg(args, "project_id")
	if project != "" {
		if _, err := s.newEngine(map[string]any{"ProjectID": project}); stderrors.Is(err, ErrProjectNotAllowed) {
			return failed(ctx, err, nil), true
		}
	}

	var j history.Journal
	for _, dir := range intent.Dirs(root) {
		part, err := history.ReadAll(history.DirOf(dir), s.historyCheck)
		if err != nil {
			return failed(ctx, err, nil), true
		}
		j.Lines, j.Skipped = append(j.Lines, part.Lines...), j.Skipped+part.Skipped
	}
	sort.SliceStable(j.Lines, func(a, b int) bool { return j.Lines[a].Time < j.Lines[b].Time })

	allowed := map[string]bool{}
	hidden := map[string]bool{}
	shown := func(l history.Line) bool {
		if project != "" && l.ProjectID != project {
			return false
		}
		if !s.projectBound {
			return true
		}
		if l.ProjectID == "" {
			hidden[l.IntentID] = true
			return false
		}
		ok, seen := allowed[l.ProjectID]
		if !seen {
			_, err := s.newEngine(map[string]any{"ProjectID": l.ProjectID})
			ok = !stderrors.Is(err, ErrProjectNotAllowed)
			allowed[l.ProjectID] = ok
		}
		return ok
	}

	lines := []historyLine{}
	for _, l := range j.Since(since) {
		if shown(l) {
			lines = append(lines, s.historyLineOf(l))
		}
	}
	truncated := len(lines) > historyMax
	if truncated {
		lines = lines[len(lines)-historyMax:]
	}

	written := map[string]bool{}
	for _, l := range j.Lines {
		written[l.IntentID] = true
	}
	live := history.Live(j.Lines)
	// A record of an intent with no line: a call killed before its end
	// wrote one. Its outcome is not known.
	for _, in := range pendingIntents(root) {
		if !written[in.ID] {
			live = append(live, history.Line{Time: in.StartedAt.UTC().Format(time.RFC3339), Tool: in.Tool, Surface: in.Surface, Key: in.Key,
				ProjectID: in.ProjectID, Zone: in.Zone, Name: in.Name, Target: in.Target, Kind: resourceKind(in.Tool),
				Outcome: history.Unknown, IntentID: in.ID})
		}
	}
	var checked *historyChecked
	results := map[int]string{}
	if boolArg(args, "check", false) {
		eng, err := s.engineOfCall(args)
		if err != nil {
			return failed(ctx, err, nil), true
		}
		var visible []int
		for i, l := range live {
			if shown(l) {
				visible = append(visible, i)
			}
		}
		checked, results = s.checkLive(ctx, live, visible, eng.Maker())
	}
	liveLines := []historyLine{}
	for i, l := range live {
		if shown(l) {
			hl := s.historyLineOf(l)
			hl.CheckResult = results[i]
			liveLines = append(liveLines, hl)
		}
	}
	liveTruncated := len(liveLines) > historyMax
	if liveTruncated {
		liveLines = liveLines[len(liveLines)-historyMax:]
	}

	ans := map[string]any{
		"journal_of": filepath.Base(root),
		"since":      since.UTC().Format(time.RFC3339),
		"lines":      lines,
		"live":       liveLines,
		"note": i18n.T("lines: the changes evo made in this folder of state since «since»; live: what may still be in the cloud — made and not removed, or with an outcome not known (outcome unknown: read the tool of check by the name or the id). A line names the resource as the call did; it does not prove the resource is there or is evo's.",
			"lines — изменения, которые evo сделал в этой папке состояния с «since»; live — что может оставаться в облаке: создано и не удалено или с неизвестным итогом (outcome unknown — прочитайте инструментом из check по имени или id). Строка называет ресурс так, как его назвал вызов; что ресурс есть и что он evo, она не доказывает."),
	}
	if truncated {
		ans["truncated"] = true
	}
	if liveTruncated {
		ans["live_truncated"] = true
	}
	if j.Skipped > 0 {
		ans["skipped"] = j.Skipped
	}
	// The lines of that day and before were pruned by age or size: what
	// is live of them is kept, the rest is gone (history.Prune).
	if day := history.RetiredThrough(history.DirOf(intent.Dir(root))); day != "" {
		ans["retired_through"] = day
	}
	if len(hidden) > 0 {
		ans["hidden_without_project"] = len(hidden)
	}
	if checked != nil {
		ans["checked"] = checked
		would := []historyLine{}
		for i, l := range live {
			if results[i] == checkNotSeen {
				would = append(would, s.historyLineOf(l))
			}
		}
		ans["would_close"] = would
		ans["check_note"] = i18n.T("would_close: a read by id, with the key that sent the call, answers «not found»: the resource was removed, or this key no longer sees it — the platform does not tell which. history writes nothing and closes nothing: the lines stay live in the journal until evo deletes the resource. check_result tells why a live line was not read.",
			"would_close — чтение по id ключом, которым отправлен вызов, отвечает «не найдено»: ресурс удалён или этот ключ его больше не видит, — какое из двух, платформа не говорит. history ничего не пишет и не закрывает: в журнале строки остаются живыми, пока ресурс не удалён через evo. check_result — почему живая строка не прочитана.")
	}
	return toJSON(ans), false
}

// historyCheck takes a line whose tool is of the registry (or evo login)
// and whose reason is a code of evo: the rest of a line is checked by its
// form (history.ReadAll).
func (s *Registry) historyCheck(l history.Line) bool {
	if _, ok := s.Lookup(l.Tool); !ok && l.Tool != "login" {
		return false
	}
	return l.Reason == "" || clierrors.IsCode(l.Reason) || l.Reason == clierrors.ReasonInterrupted || l.Reason == clierrors.ReasonGeneral
}

// historyLineOf is l as history answers it, with the read that tells an
// outcome not known.
func (s *Registry) historyLineOf(l history.Line) historyLine {
	out := historyLine{Time: l.Time, Tool: l.Tool, Surface: l.Surface, ProjectID: l.ProjectID, Zone: l.Zone, Name: l.Name,
		Target: l.Target, Kind: l.Kind, Parent: l.Parent, ResourceID: l.ResourceID, Outcome: l.Outcome, Reason: l.Reason, OperationID: l.OperationID,
		IntentID: l.IntentID, JobID: l.JobID}
	if l.Outcome == history.Unknown && l.Tool != "login" {
		out.Check = s.listToolOf(l.Tool)
	}
	return out
}

// historyCheckMax is the most reads one history with check sends: a
// journal of many live lines is checked over several calls, not in one
// burst of reads. Only a line that passed every local condition spends it,
// the oldest first («Риск», conclusion 315).
const historyCheckMax = 50

// What history with check found of a live line (check_result). check only
// says: history is a read and writes nothing, the journal included
// («Риск», conclusion 315, У4).
const (
	// checkNotSeen: the read by id, with the key that sent the call,
	// answers «not found» — the resource was removed, or this key no
	// longer sees it; which, the platform does not tell (У3). The line is
	// in would_close.
	checkNotSeen = "not_seen"
	// checkThere: the read by id finds the resource.
	checkThere = "there"
	// checkReadFailed: the read answered neither «there» nor «not found»
	// (403, 401, 429, 5xx, a cut): not checked.
	checkReadFailed = "read_failed"
	// checkOtherKey: the call was sent with another key, which may see
	// what this one does not (R-82).
	checkOtherKey = "other_key"
	// checkNoKey: the line or this run names no key.
	checkNoKey = "no_key"
	// checkNoRead: the kind has no read by id.
	checkNoRead = "no_read_by_id"
	// checkNotByID: the line has no id from the cloud — an outcome not
	// known, whose id may be the name of the call's argument (У2 of 311).
	checkNotByID = "not_by_id"
	// checkNoProject: the line names no project — a call by id (R-82).
	checkNoProject = "no_project"
	// checkInParent: the resource lives in a parent, which a read by its
	// id does not take (У5).
	checkInParent = "in_parent"
	// checkTooNew: the line is younger than madeListGrace: a read may not
	// show a resource just made yet (У1).
	checkTooNew = "too_new"
	// checkLater: a later live line names the same resource; that one is
	// read (У2).
	checkLater = "later_line"
	// checkOverBudget: not read in this call (historyCheckMax).
	checkOverBudget = "over_budget"
)

// historyChecked counts what history with check did: the reads sent, the
// lines it would close, and the lines not read, by why.
type historyChecked struct {
	Read       int            `json:"read"`
	WouldClose int            `json:"would_close"`
	NotChecked map[string]int `json:"not_checked,omitempty"`
}

// checkLive reads by id each live line of the indexes visible (oldest
// first) that names a resource of a project by its id from the cloud
// (created, found), with the rule of doctor (readByID) and the key cur of
// this run, and tells what it found (the ТЗ of the journal, У7; «Риск»,
// conclusion 315). It writes nothing: a line the read does not see is
// would_close. The cloud is only read.
func (s *Registry) checkLive(ctx context.Context, live []history.Line, visible []int, cur *state.Maker) (*historyChecked, map[int]string) {
	out := &historyChecked{NotChecked: map[string]int{}}
	results := map[int]string{}
	resource := func(l history.Line) string { return l.Kind + "\x00" + l.Parent + "\x00" + l.ResourceID }
	latest := map[string]int{}
	for _, i := range visible {
		latest[resource(live[i])] = i
	}
	fresh := clock.Now().Add(-madeListGrace)
	for _, i := range visible {
		l := live[i]
		at, _ := time.Parse(time.RFC3339, l.Time)
		k := cardOf(l.Tool)
		why := ""
		switch {
		case (l.Outcome != history.Created && l.Outcome != history.Found) || l.ResourceID == "":
			why = checkNotByID
		case l.ProjectID == "":
			why = checkNoProject
		case l.Parent != "":
			why = checkInParent
		case latest[resource(l)] != i:
			why = checkLater
		case at.After(fresh):
			why = checkTooNew
		default:
			why = map[readSkip]string{readNoCard: checkNoRead, readOtherKey: checkOtherKey, readNoKey: checkNoKey}[readSkipOf(k, l.Key, cur)]
		}
		if why == "" && out.Read == historyCheckMax {
			why = checkOverBudget
		}
		if why != "" {
			results[i] = why
			out.NotChecked[why]++
			continue
		}
		out.Read++
		_, notSeen, _, err := s.readByID(ctx, k, l.ResourceID, l.Key, cur)
		switch {
		case notSeen:
			results[i] = checkNotSeen
			out.WouldClose++
		case err == nil:
			results[i] = checkThere
		default:
			results[i] = checkReadFailed
			out.NotChecked[checkReadFailed]++
		}
	}
	return out, results
}

// cardOf is the kind of the resource tool makes or removes, as its delete
// addresses it by id: its read by id tells whether the resource is there
// (readByID). nil when the delete names none.
func cardOf(tool string) *ownerKind {
	del := tool
	if c, ok := createdOf(tool); ok && c.delete != "" {
		del = c.delete
	}
	d, ok := deleteOf(del)
	if !ok {
		return nil
	}
	for _, a := range decls[del].IDArgs {
		if a.name == d.arg {
			return a.kind
		}
	}
	return nil
}
