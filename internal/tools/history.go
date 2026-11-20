// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// historyOutcome is what the answer of a call of tool name with args says
// for the journal of changes (D43): one reading of an answer for every
// point that ends a record of an intent. A line closes only on an outcome
// that is certain; in doubt it stays live («Риск», conclusion 300):
//
//   - a refusal is an error — unless the answer names what the call made
//     (a VM in an error state, a stack in part): that is unknown, with its
//     id;
//   - «already deleted» is said as such (D26);
//   - another «already» of a create — the resource of the name is there —
//     is found, with its id; of any other tool, a change that was not
//     needed;
//   - a delete that goes on (deleting, a next step) is unknown, with the
//     ids; one that ended removed its resource;
//   - a create made the resource its declaration names (createdRef); a
//     create whose answer names none is unknown, not a change;
//   - a stack is told by its job (the job_id of the answer);
//   - any other answer is a change of a resource that is there.
//
// The kind of the resource goes with its id (resourceKind), so a reader
// tells the lines of one resource from those of another of the same id
// or name.
//
// A point whose outcome is not known (intent.End, intent.Hold) takes the
// same reading and says unknown itself.
func historyOutcome(name string, args map[string]any, answer string) history.Outcome {
	var res map[string]any
	_ = json.Unmarshal([]byte(answer), &res)
	out := history.Outcome{OperationID: stringArg(res, "operation_id"), JobID: stringArg(res, "job_id"), Kind: resourceKind(name)}
	if arg := resourceParent(name); arg != "" && stringArg(args, arg) != "" {
		out.Parent = arg + "=" + stringArg(args, arg)
	}
	status := answerStatus(answer)
	_, isCreate := createdOf(name)
	d, isDelete := deleteOf(name)
	made := func() string {
		if !isCreate {
			return ""
		}
		if ref := createdRef(name, args, res); ref != "" {
			return ref
		}
		details, _ := res["details"].(map[string]any)
		return createdRef(name, args, details)
	}
	// What a refusal says the call made is in its answer only: a create
	// named by its argument (secret_create's path) has the name whatever
	// the cloud did, and a refusal that made nothing names none («Риск»,
	// conclusion 300, finding 300-1).
	madeByAnswer := func() string {
		if !isCreate {
			return ""
		}
		if id := createdID(name, res); id != "" {
			return id
		}
		details, _ := res["details"].(map[string]any)
		return createdID(name, details)
	}
	// A stack is told by its job: its steps go round the record of an
	// intent (journalDepthKey), so the line of the job keeps what the
	// stack made live — made while its job runs or is done, gone when it
	// is destroyed, unknown otherwise («Риск», conclusion 300, У6; Астра,
	// #2166).
	// The job is the answer's, or the call's: stack_destroy names it by
	// its argument only.
	job := stringArg(res, "job_id")
	if job == "" {
		job = stringArg(args, "job_id")
	}
	// Only a tool of the stacks is told by its job: another answer that
	// names a job_id is not a stack's («Риск», 300-2).
	if job != "" && groupOf(name) == "stack" {
		out.Kind, out.ResourceID, out.JobID = "stack", job, job
		out.Reason = answerReason(answer)
		switch status {
		case "running", "done":
			out.Result = history.Created
		case "destroyed", "already_destroyed":
			out.Result = history.Deleted
		default:
			out.Result = history.Unknown
		}
		switch {
		case out.Reason != "" && status == "" && (out.Reason == clierrors.CodeNotFound || out.Reason == clierrors.CodeUsage):
			// The call was refused whole, with no job status: a job_id
			// mistyped, a call refused before any step — nothing made
			// («Риск», 300-1).
			out.Result, out.ResourceID = history.Error, ""
		case out.Reason != "":
			out.Result = history.Unknown
		}
		return out
	}
	deleteID := stringArg(args, d.arg)
	if name == "destroy" {
		deleteID = firstNonEmptyStr(stringArg(res, "deleted_vm"), stringArg(res, "vm_id"), deleteID)
	}
	if name == "pg_delete" {
		deleteID = firstNonEmptyStr(deleteID, stringArg(args, "cluster_id"))
	}
	switch reason := answerReason(answer); {
	case name == "destroy" && (status == "partial" || boolArg(res, "cleanup_pending", false)):
		out.Result, out.ResourceID, out.Reason = history.Unknown, deleteID, reason
	case reason != "" && intent.OutcomeUnknown(reason, answerHTTPStatus(answer)):
		// Cut after it sent — the network, a time limit, Ctrl-C: what it
		// made may be there, named by its argument too («Риск», conclusion
		// 311, У2). A call that sent no write at all is an error at its
		// end (beginIntent), whatever this says.
		out.Result, out.Reason, out.ResourceID = history.Unknown, reason, made()
	case reason != "":
		if id := madeByAnswer(); id != "" {
			out.Result, out.Reason, out.ResourceID = history.Unknown, reason, id
			break
		}
		out.Result, out.Reason = history.Error, reason
	case status == "already_deleted":
		out.Result = history.AlreadyDeleted
		if isDelete {
			out.ResourceID = deleteID
		}
	case isDelete:
		// The id the delete takes: the same createdRef wrote in the line
		// of the create, so the two meet (history.Live).
		out.ResourceID = deleteID
		// A delete that goes on — its status a doing («deleting»), or a
		// next step to read — has not removed its resource yet.
		if strings.HasSuffix(status, "ing") || stringArg(res, "next_tool") != "" {
			out.Result = history.Unknown
		} else {
			out.Result = history.Deleted
		}
	case isCreate:
		id := made()
		switch {
		case id == "":
			out.Result = history.Unknown
		case strings.HasPrefix(status, "already_"):
			out.Result, out.ResourceID = history.Found, id
		default:
			out.Result, out.ResourceID = history.Created, id
		}
	default:
		out.Result = history.Changed
	}
	return out
}

// resourceKind is the kind of the resource tool makes or removes: what its
// delete names it ("disk", "cluster"); "" for a tool that neither makes
// nor removes one.
func resourceKind(tool string) string {
	if d, ok := deleteOf(tool); ok {
		return d.what
	}
	if c, ok := createdOf(tool); ok && c.delete != "" {
		if d, ok := deleteOf(c.delete); ok {
			return d.what
		}
	}
	return ""
}

// resourceParent is the argument that names the resource the resource of
// tool lives in: the parent its delete declares (a cluster, a bucket); ""
// for a resource of a project.
func resourceParent(tool string) string {
	if d, ok := deleteOf(tool); ok {
		return d.parent
	}
	if c, ok := createdOf(tool); ok && c.delete != "" {
		if d, ok := deleteOf(c.delete); ok {
			return d.parent
		}
	}
	return ""
}

// settledOf is the line of a record that the answered call out of the
// same tool settles (intent.Settle): what that call made or found is what
// the cut one made — found, with its id; what it removed, removed; a
// change, a change. An outcome without the id it would need stays unknown.
func settledOf(out history.Outcome) history.Outcome {
	switch out.Result {
	case history.Created, history.Found:
		if out.ResourceID != "" {
			out.Result = history.Found
			return out
		}
	case history.Deleted, history.AlreadyDeleted, history.Changed:
		return out
	}
	out.Result = history.Unknown
	return out
}

// settledOutcome is the line of a record whose outcome a read of evo now
// tells (checkByList, checkByID): a delete took effect — deleted, with the
// id of its target; a create is listed — found, with the id the list
// gives, or the name its declaration names it by (createdRef). A create
// listed with no such id stays unknown: what was made is there, and live.
func settledOutcome(in intent.Intent, listedID string) history.Outcome {
	out := history.Outcome{Kind: resourceKind(in.Tool)}
	// The record names the parent as its target, when the call named one.
	if arg := resourceParent(in.Tool); arg != "" && strings.HasPrefix(in.Target, arg+"=") {
		out.Parent = in.Target
	}
	if _, isDelete := deleteOf(in.Tool); isDelete {
		// A delete in a parent names the parent as its target, and its
		// resource by name; any other, the resource by its target.
		_, id, _ := strings.Cut(in.Target, "=")
		if out.Parent != "" {
			id = in.Name
		}
		out.Result, out.ResourceID = history.Deleted, id
		return out
	}
	id := listedID
	if c, ok := createdOf(in.Tool); ok && c.arg != "" {
		id = in.Name
	}
	if id == "" {
		out.Result = history.Unknown
		return out
	}
	out.Result, out.ResourceID = history.Found, id
	return out
}

// pruneJournal keeps the journal of changes beside the records of dir in
// its bounds (history.Prune), each line checked as history checks it
// (historyCheck); a pruning that fails is said once, and the call answers
// as it would.
func (r *Registry) pruneJournal(dir string) {
	if _, err := history.Prune(history.DirOf(dir), clock.Now(), r.historyCheck); err != nil {
		config.Warn(i18n.Tf("the journal of changes in %s was not pruned (%v); it is pruned the next time",
			"журнал изменений %s не очищен (%v); очистка — в следующий раз", history.DirOf(dir), err))
	}
}

// journalCheck is doctor's check of the journal of changes of dir: its
// size, its oldest day and its live lines; a warning when the live lines
// alone are over the ceiling — none is removed, and the journal grows. No
// check for a journal that is not there.
func (r *Registry) journalCheck(dir string) (DoctorCheck, bool) {
	s, err := history.Describe(dir, r.historyCheck)
	if err != nil {
		return DoctorCheck{Name: "journal of changes", Status: CheckWarn, Detail: CheckDetail(err)}, true
	}
	if s.Bytes == 0 {
		return DoctorCheck{}, false
	}
	c := DoctorCheck{Name: "journal of changes", Status: CheckOK,
		Detail: i18n.Tf("%s: %d KB since %s, %d live lines (made and not removed, or an outcome not known)",
			"%s: %d КБ с %s, живых строк — %d (создано и не удалено или итог неизвестен)", dir, (s.Bytes+1023)/1024, s.Oldest, s.Live)}
	if len(s.Deferred) > 0 {
		// A pruning put off is said, with the files in the way and what
		// they hold: the journal grows, and the person and the agent know
		// why. No file is to be removed while it holds a live line — a
		// resource made and not removed («Риск» to 324-3).
		c.Status, c.Files = CheckWarn, s.Deferred
		var files []string
		for _, f := range s.Deferred {
			if f.Name == history.LiveName {
				// Lines of LiveName not read are evo's own when the mark of
				// the days pruned is gone: they are not said to be another's,
				// and LiveName is never to be removed («Риск», 358, У4).
				files = append(files, i18n.Tf("%s: %d lines not read — the mark of the days pruned is gone or older than them; %d lines evo did not write, %d live; %s is not to be moved or removed whatever the counts",
					"%s: строк не читается — %d: метки вычищенных дней нет или она старше их; строк не от evo — %d, живых — %d; %s не переносить и не удалять ни при каком счёте", f.Name, f.Unread, f.NotEvos, f.Live, f.Name))
				continue
			}
			files = append(files, i18n.Tf("%s: %d lines evo did not write, %d live", "%s: строк не от evo — %d, живых — %d", f.Name, f.NotEvos, f.Live))
		}
		c.Detail += i18n.Tf("; the pruning is put off: going on would change what history finds live — nothing is removed and the journal grows; it tries again each day (%s). history shows what is live. A file with live lines is not to be moved or removed: a live line is a resource made and not removed, or an outcome not known",
			"; очистка отложена: продолжение изменило бы то, что history находит живым, — ничего не удаляется, и журнал растёт; очистка пробует снова каждый день (%s). Живое показывает history. Файл с живыми строками не переносить и не удалять: живая строка — ресурс, созданный и не удалённый, или неизвестный итог", strings.Join(files, "; "))
	}
	if s.Unfinished {
		// A pruning that did not fit its limit of time is said: the journal
		// it was to prune grows («Риск», conclusion 356, У2).
		c.Status = CheckWarn
		c.Detail += i18n.T("; the last pruning did not fit its limit of time (a large journal): nothing was removed, and it tries again the next day",
			"; последняя очистка не уложилась в свой предел времени (журнал велик): ничего не удалено, очистка попробует снова на следующий день")
	}
	if s.OverCeiling {
		c.Status = CheckWarn
		c.Detail += i18n.T("; the live lines alone are over the ceiling of 64 MB: none is removed, and the journal grows — a line closes when evo deletes its resource",
			"; одни живые строки больше потолка 64 МБ: ни одна не удаляется, и журнал растёт — строка закрывается, когда ресурс удалён через evo")
	}
	return c, true
}
