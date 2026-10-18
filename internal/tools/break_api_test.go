// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// An HTTP 200 with an HTML body (a gateway page) was reported as deleted,
// created or ok (break r3b, R3B-1). docs/exit-codes.md: an API answer is
// API_ERROR (exit 5); httpretry and the compute adapter read it as a 502.
// An empty 200 stays a success: see contractAPIBreak.
func TestAPI_HTML200IsAPIError(t *testing.T) {
	for _, tc := range []struct{ name, kind string }{
		{"image_delete", "html"},
		{"pg_db_create", "html"},
		{"ca_start", "html"},
	} {
		rt := &contractRT{kind: tc.kind}
		reg := contractReg(t, rt)
		tl, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatal(tc.name)
		}
		args := contractArgs(tl)
		args["confirmed"] = true
		text, isErr, pan := contractCall(tl, args, time.Second)
		reason, status := contractReason(text)
		if pan != "" || !isErr || reason != clerrors.CodeAPIError || status == "deleted" || status == "created" || status == "ok" {
			t.Errorf("%s %s: err=%v reason=%s status=%s text=%.220s", tc.name, tc.kind, isErr, reason, status, text)
		}
	}
}

// The cloud answered, and the body is not JSON: the reason was USAGE, exit 1
// (break r3b, R3B-2). docs/exit-codes.md: USAGE is a local refusal, nothing
// was sent; an API answer is API_ERROR, exit 5.
func TestAPI_UnparsedBodyIsAPIError(t *testing.T) {
	for _, tc := range []struct{ name, kind string }{
		{"disk_resize", "html"},
		{"disk_resize", "empty"},
		{"tg_list", "html"},
		{"snat_create", "html"},
	} {
		rt := &contractRT{kind: tc.kind}
		reg := contractReg(t, rt)
		tl, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatal(tc.name)
		}
		args := contractArgs(tl)
		if tl.Mutating {
			args["confirmed"] = true
		}
		text, isErr, pan := contractCall(tl, args, time.Second)
		reason, _ := contractReason(text)
		if pan != "" || !isErr || reason != clerrors.CodeAPIError || strings.Contains(text, `"reason": "USAGE"`) {
			t.Errorf("%s %s: err=%v reason=%s text=%.220s", tc.name, tc.kind, isErr, reason, text)
		}
	}
}
