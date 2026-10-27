// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/journal"
)

// outer is the outermost layer of a tool's handler, in New and in Wrap
// alike: the session journal over the check of the arguments, so a call
// the check refuses is recorded too (D46).
func outer(name string, schema map[string]any, h Handler) Handler {
	return journaled(name, schema, checkedArgs(name, schema, h))
}

type journalDepthKey struct{}

// journaled records each call of the tool in the session journal (D46):
// the names of the arguments its schema declares, consent, the reason of
// an error, the duration and the size of the answer — no value and no
// text of the answer. A call the tool makes through the registry (a step
// of a stack) is a record one level deeper.
func journaled(name string, schema map[string]any, next Handler) Handler {
	props, _ := schema["properties"].(map[string]any)
	return func(ctx context.Context, args map[string]any) (string, bool) {
		depth, _ := ctx.Value(journalDepthKey{}).(int)
		start := time.Now()
		text, isErr := next(context.WithValue(ctx, journalDepthKey{}, depth+1), args)
		if !journal.Active() {
			return text, isErr
		}
		rec := journal.Record{Tool: name, Depth: depth, Error: isErr,
			MS: time.Since(start).Milliseconds(), Bytes: len(text)}
		for k := range args {
			if _, ok := props[k]; ok {
				rec.Args = append(rec.Args, k)
			} else {
				rec.UnknownArgs++
			}
		}
		sort.Strings(rec.Args)
		rec.Confirmed, _ = args["confirmed"].(bool)
		var ans struct{ Status, Reason string }
		if json.Unmarshal([]byte(text), &ans) == nil {
			rec.Preview = ans.Status == "confirmation_required"
			if isErr {
				rec.Reason = journalReason(ans.Reason)
			}
		}
		journal.Write(rec)
		return text, isErr
	}
}

// journalReason keeps a reason that is a code of internal/errors; any
// other text is the caller's or the cloud's, and stays out.
func journalReason(r string) string {
	switch {
	case r == "":
		return ""
	case clierrors.IsCode(r), r == clierrors.ReasonInterrupted, r == clierrors.ReasonGeneral:
		return r
	}
	return "OTHER"
}
