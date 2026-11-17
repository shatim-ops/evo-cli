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
			live = append(live, history.Line{Time: in.StartedAt.UTC().Format(time.RFC3339), Tool: in.Tool, Surface: in.Surface,
				ProjectID: in.ProjectID, Zone: in.Zone, Name: in.Name, Target: in.Target, Kind: resourceKind(in.Tool),
				Outcome: history.Unknown, IntentID: in.ID})
		}
	}
	liveLines := []historyLine{}
	for _, l := range live {
		if shown(l) {
			liveLines = append(liveLines, s.historyLineOf(l))
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
	if len(hidden) > 0 {
		ans["hidden_without_project"] = len(hidden)
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
