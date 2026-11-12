// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// What disk_create's preview reads beyond the planner's checks (the disk
// type's limits and the price) is read for the question only
// (Plan.PreviewOf): the call without consent makes the planner's reads and
// then the preview's; the call with confirmed=true — which shows no
// preview — makes the planner's reads and the create, and none of the
// preview's. Before the gate the preview stood in the branch without
// confirmed; a planner that built it at once would make its requests on
// every consented create.
func TestDiskCreate_PreviewReadsAreForTheQuestionOnly(t *testing.T) {
	args := func(confirmed bool) map[string]any {
		a := map[string]any{"name": "data", "size_gb": 10, "zone": "ru.AZ-1"}
		if confirmed {
			a["confirmed"] = true
		}
		return a
	}
	asked := &faultAPI{}
	r := budgetAPIRegistry(t, asked)
	text, isErr := r.handlers["disk_create"](context.Background(), args(false))
	if !isErr || !strings.Contains(text, "confirmation_required") {
		t.Fatalf("without consent: %s", text)
	}
	previewed := asked.callLog()

	made := &faultAPI{}
	r = budgetAPIRegistry(t, made)
	text, isErr = r.handlers["disk_create"](context.Background(), args(true))
	if isErr {
		t.Fatalf("with consent: %s", text)
	}
	var planned []string
	for _, op := range made.callLog() {
		if !strings.HasPrefix(op, "GET ") {
			break
		}
		planned = append(planned, op)
	}
	if len(planned) == 0 || len(planned) >= len(previewed) || !slices.Equal(planned, previewed[:len(planned)]) {
		t.Errorf("the consented create read %v before its write;\nthe preview read %v:\nwant the planner's reads alone — the preview's own reads are not made with consent", planned, previewed)
	}
}
