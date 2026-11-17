// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/history"
)

// historyOutcome is what the answer of a call of tool name with args says
// for the journal of changes (D43): one reading of an answer for every
// point that ends a record of an intent. A refusal is an error with its
// code; «already deleted» is said as such (D26); another «already» — the
// resource is as the call asks — is found; a delete removed its resource;
// a create made the one its declaration names (createdRef); any other
// answer is a change of a resource that is there. The ids are the
// answer's: the resource, the operation, the job of a stack.
//
// A point whose outcome is not known (intent.End, intent.Hold) takes the
// same reading and says unknown itself.
func historyOutcome(name string, args map[string]any, answer string) history.Outcome {
	var res map[string]any
	_ = json.Unmarshal([]byte(answer), &res)
	out := history.Outcome{OperationID: stringArg(res, "operation_id"), JobID: stringArg(res, "job_id")}
	status := answerStatus(answer)
	switch reason := answerReason(answer); {
	case reason != "":
		out.Result, out.Reason = history.Error, reason
	case status == "already_deleted":
		out.Result = history.AlreadyDeleted
		if d, isDelete := deleteOf(name); isDelete {
			out.ResourceID = stringArg(args, d.arg)
		}
	case strings.HasPrefix(status, "already_"):
		out.Result = history.Found
	default:
		if d, isDelete := deleteOf(name); isDelete {
			// The id the delete takes: the same createdRef wrote in
			// the line of the create, so the two meet (history.Live).
			out.Result, out.ResourceID = history.Deleted, stringArg(args, d.arg)
			break
		}
		if decls[name].Created.declared() {
			if ref := createdRef(name, args, res); ref != "" {
				out.Result, out.ResourceID = history.Created, ref
				break
			}
		}
		out.Result = history.Changed
	}
	return out
}
