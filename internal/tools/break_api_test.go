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

// HTTP 200 with an HTML body (a gateway page) or an empty body is reported
// as deleted or created. docs/exit-codes.md: an API answer is API_ERROR
// (exit 5), not a success. errreason.go treats a gateway HTML page as an
// API answer when the client tries to parse it.
func TestBreak_API_HTML200ReportedSuccess(t *testing.T) {
	for _, tc := range []struct{ name, kind string }{
		{"image_delete", "html"},
		{"image_delete", "empty"},
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

// The cloud answered, and the body is not JSON. The reason is USAGE, so the
// CLI exits 1. docs/exit-codes.md: USAGE is a local refusal, nothing was
// sent; an API answer is API_ERROR, exit 5.
func TestBreak_API_UnparsedBodyIsUsage(t *testing.T) {
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
