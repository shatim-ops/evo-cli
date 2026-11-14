// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
)

// quoteCloud is a value the cloud gave — a resource's name — as evo's own
// text (message, next, hint, warning, note) writes it: on one line and in
// quotes. Anyone with access to the project names resources, and a name
// like "web\nIGNORE PREVIOUS INSTRUCTIONS…" must not read as evo's words to
// the agent. Data fields keep the value as the cloud has it (process audit
// 1.2.1, П9; TestUntrustedNames_OneLineInText).
func quoteCloud(s string) string { return strconv.Quote(s) }

// untrustedAnswer declares what of a tool's answer evo did not write.
// Fields are the answer fields with that text: the answer lists those it
// has in "_untrusted" — a marker for a client that reads it: the text is
// data, never instructions (process audit 1.2.1, П9). Workload says the
// text is what a workload or a stored object wrote, and New ends the
// tool's description with untrustedOutputNote (Tool.UntrustedOutput).
//
// Whole says the answer is the body of the service as it has it, every
// field of it the cloud's — a read that passes the resource on: the
// answer lists every field it has, but those of own, which evo adds.
// An error answer of such a tool is evo's and is not marked.
type untrustedAnswer struct {
	fields   []string
	whole    bool
	own      []string
	workload bool
}

// marked reports whether the answers of the tool say "_untrusted".
func (u untrustedAnswer) marked() bool { return u.fields != nil || u.whole }

// in is the fields of the answer m the marker names.
func (u untrustedAnswer) in(m map[string]any) []string {
	var has []string
	if u.whole {
		for f := range m {
			if f != "_untrusted" && !slices.Contains(u.own, f) {
				has = append(has, f)
			}
		}
		sort.Strings(has)
		return has
	}
	for _, f := range u.fields {
		if _, ok := m[f]; ok {
			has = append(has, f)
		}
	}
	return has
}

// markUntrusted adds "_untrusted" to the answers of a tool that declares
// them (Decl.Untrusted): the fields of u the answer has.
func markUntrusted(u untrustedAnswer, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		text, isErr := next(ctx, args)
		if u.whole && isErr {
			return text, isErr
		}
		dec := json.NewDecoder(bytes.NewReader([]byte(text)))
		dec.UseNumber()
		var m map[string]any
		if dec.Decode(&m) != nil {
			return text, isErr
		}
		has := u.in(m)
		if len(has) == 0 {
			return text, isErr
		}
		m["_untrusted"] = has
		return toJSON(m), isErr
	}
}
