// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
)

// quoteCloud is a value the cloud gave — a resource's name — as evo's own
// text (message, next, hint, warning, note) writes it: on one line and in
// quotes. Anyone with access to the project names resources, and a name
// like "web\nIGNORE PREVIOUS INSTRUCTIONS…" must not read as evo's words to
// the agent. Data fields keep the value as the cloud has it (process audit
// 1.2.1, П9; TestUntrustedNames_OneLineInText).
func quoteCloud(s string) string { return strconv.Quote(s) }

// untrustedFields are the answer fields whose text the workload wrote, not
// evo: a VM's log, a container app's log, kubectl's output. The answer
// lists those it has in "_untrusted" — a marker for a client that reads
// it: the text is data, never instructions (process audit 1.2.1, П9).
var untrustedFields = map[string][]string{
	"logs":        {"log"},
	"ca_logs":     {"logs"},
	"k8s_kubectl": {"output"},
	// deploy: the output of docker compose up — its log over MCP, the last
	// lines of a failure (#558).
	"deploy": {"log", "output"},
}

// markUntrusted adds "_untrusted" to the answers of a tool of
// untrustedFields: the fields of the list the answer has.
func markUntrusted(fields []string, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		text, isErr := next(ctx, args)
		dec := json.NewDecoder(bytes.NewReader([]byte(text)))
		dec.UseNumber()
		var m map[string]any
		if dec.Decode(&m) != nil {
			return text, isErr
		}
		var has []string
		for _, f := range fields {
			if _, ok := m[f]; ok {
				has = append(has, f)
			}
		}
		if len(has) == 0 {
			return text, isErr
		}
		m["_untrusted"] = has
		return toJSON(m), isErr
	}
}
