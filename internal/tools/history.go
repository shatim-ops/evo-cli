// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/history"
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
//   - a stack in part (partial) is unknown;
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
	switch reason := answerReason(answer); {
	case reason != "":
		if id := madeByAnswer(); id != "" {
			out.Result, out.Reason, out.ResourceID = history.Unknown, reason, id
			break
		}
		out.Result, out.Reason = history.Error, reason
	case status == "partial":
		out.Result = history.Unknown
	case status == "already_deleted":
		out.Result = history.AlreadyDeleted
		if isDelete {
			out.ResourceID = stringArg(args, d.arg)
		}
	case isDelete:
		// The id the delete takes: the same createdRef wrote in the line
		// of the create, so the two meet (history.Live).
		out.ResourceID = stringArg(args, d.arg)
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
	if _, isDelete := deleteOf(in.Tool); isDelete {
		_, id, _ := strings.Cut(in.Target, "=")
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
