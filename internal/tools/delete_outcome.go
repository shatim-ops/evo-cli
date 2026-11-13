// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// A delete answers how it ended, not only that it started (D74, as D66 for
// dns_zone_delete; axis 3: «deleted» is a resource that is gone). The
// delete reads its operation for up to opWaitMaxSeconds, within the call's
// budget (D69), the same in the CLI and in MCP: done is deleted, an error is
// a refusal with Cloud.ru's text, a wait the caller cancelled is
// INTERRUPTED naming the delete already started, an operation still
// running — or one that cannot be read — is deleting with the tool that
// shows its end.

// deleteWatch is a started delete whose end the answer waits for.
type deleteWatch struct {
	tool  string         // the delete tool, a key of deleteTargets
	ref   string         // the resource as the call named it
	ident map[string]any // the fields naming the resource in the answer
	opID  string         // the operation of the delete
	// read reads the operation, in the form of *_operation's answer.
	read func(context.Context) (map[string]any, error)
	// next is the tool that shows the end of a delete still running, with
	// its arguments.
	next     string
	nextArgs map[string]any
	// hint is where the person sees the resource after a refusal; "" is
	// the console or Cloud.ru's support.
	hintEN, hintRU string
}

// deleteOutcomeWait is how long, in seconds, a delete waits for its
// operation. A variable only so that tests can shorten it.
var deleteOutcomeWait = opWaitMaxSeconds

// deleteOutcome is the answer of the delete w after the wait for its end;
// with wait=false in the call's args it is deleting at once, reading
// nothing (a script deleting many keeps its speed; the owner's decision
// 03.11).
func deleteOutcome(ctx context.Context, args map[string]any, w deleteWatch) (string, bool) {
	// A delete with no operation (goneWatch: the resource is read until it
	// is gone) names none: an empty operation_id read as one to wait for.
	fields := map[string]any{}
	if w.opID != "" {
		fields["operation_id"] = w.opID
	}
	maps.Copy(fields, w.ident)
	if waitOff(args) {
		return deleting(fields, w)
	}
	m, err := waitForOpJSON(ctx, deleteOutcomeWait, w.read)
	if errors.Is(err, context.Canceled) {
		// The delete is started: the answer names it (FB160-14).
		return errAnswer(err, fields), true
	}
	if err == nil && m != nil {
		if msg, code, failed := opFailure(m["error"]); failed {
			return errResult(deleteRefusal(w, msg, code)), true
		}
		if done, _ := m["done"].(bool); done {
			fields["status"] = "deleted"
			return toJSON(fields), false
		}
	}
	return deleting(fields, w)
}

// deleting is the answer of the delete w still under way: its fields and
// the tool that shows its end.
func deleting(fields map[string]any, w deleteWatch) (string, bool) {
	fields["status"] = "deleting"
	fields["next"] = fmt.Sprintf("%s %s shows when the delete ends", w.next, argsLine(w.nextArgs))
	nextStep(fields, w.next, w.nextArgs)
	return toJSON(fields), false
}

// opFailure reads the error of an operation: an object with message and
// code, or a text.
func opFailure(v any) (msg, code any, failed bool) {
	switch e := v.(type) {
	case map[string]any:
		return e["message"], e["code"], len(e) > 0
	case string:
		return e, nil, strings.TrimSpace(e) != ""
	}
	return nil, nil, false
}

// deleteRefusal is the error of a delete Cloud.ru's operation ended with.
// The platform's words and code are data, quoted on one line in the
// sentence of evo (quoteCloud), as at k8s_nodepool_scale (FB19I-8).
func deleteRefusal(w deleteWatch, msg, code any) error {
	quoted := quoteCloud(fmt.Sprint(msg))
	t := deleteTargets[w.tool]
	what := i18n.T(t.what, t.whRU)
	hint := i18n.T(w.hintEN, w.hintRU)
	if w.hintEN == "" {
		hint = i18n.Tf("Delete it in the console; if Cloud.ru keeps refusing, contact Cloud.ru support with its id %s and the operation %s.",
			"Удалите его в личном кабинете; если Cloud.ru снова откажет, обратитесь в поддержку Cloud.ru с его id %s и операцией %s.", w.ref, w.opID)
	}
	if code == nil {
		return clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("Cloud.ru did not delete %s %s: %s (operation %s)", "Cloud.ru не удалил %s %s: %s (операция %s)",
				what, w.ref, quoted, w.opID), hint)
	}
	return clierrors.New(clierrors.CodeAPIError,
		i18n.Tf("Cloud.ru did not delete %s %s: %s (code %s, operation %s)", "Cloud.ru не удалил %s %s: %s (код %s, операция %s)",
			what, w.ref, quoted, quoteCloud(fmt.Sprint(code)), w.opID), hint)
}

// argsLine is args as a call's text: key=value, in the order of the keys.
func argsLine(args map[string]any) string {
	parts := make([]string, 0, len(args))
	for _, k := range slices.Sorted(maps.Keys(args)) {
		parts = append(parts, fmt.Sprintf("%s=%v", k, args[k]))
	}
	return strings.Join(parts, " ")
}

// opWatch is a deleteWatch whose operation is read by get and later by the
// *_operation tool opTool.
func opWatch[T any](tool, ref string, ident map[string]any, opID, opTool string, get func(context.Context, string) (T, error)) deleteWatch {
	return deleteWatch{tool: tool, ref: ref, ident: ident, opID: opID,
		read: func(ctx context.Context) (map[string]any, error) {
			op, err := get(ctx, opID)
			if err != nil {
				return nil, err
			}
			return opToMap(op), nil
		},
		next: opTool, nextArgs: map[string]any{"operation_id": opID, "wait_seconds": opWaitMaxSeconds}}
}

// goneWatch is a deleteWatch for a resource whose delete has no operation
// to read: the resource itself is read until it is gone, and getTool shows
// it later.
func goneWatch(tool, ref string, ident map[string]any, opID, getTool string, get func(context.Context) (map[string]any, error)) deleteWatch {
	arg := deleteTargets[tool].arg
	return deleteWatch{tool: tool, ref: ref, ident: ident, opID: opID,
		read: func(ctx context.Context) (map[string]any, error) {
			m, err := get(ctx)
			if err != nil && isGone(err) {
				return map[string]any{"done": true}, nil
			}
			if err != nil {
				return nil, err
			}
			// The resource's state is the wait line's, unless it reads as
			// the end of an operation: a resource still there is not done.
			state := map[string]any{"done": false}
			s, _ := m["status"].(string)
			if s == "" {
				s, _ = m["state"].(string)
			}
			if s != "" && !opTerminalStates[strings.ToUpper(s)] {
				state["state"] = s
			}
			return state, nil
		},
		next: getTool, nextArgs: map[string]any{arg: ref}}
}
