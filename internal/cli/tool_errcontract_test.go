// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// oldErrResult is the error answer of the tools before the code moved from
// the text to "reason" (ревью r5 M5): "error" was err.Error() as is, reason
// and hint came only from a CLIError, usage errors were "USAGE: …".
func oldErrResult(err error) string {
	res := map[string]any{"status": "error", "error": err.Error()}
	var ce *clerrors.CLIError
	if stderrors.As(err, &ce) {
		if ce.Code != "" {
			res["reason"] = ce.Code
		}
		if ce.Hint != "" {
			res["hint"] = ce.Hint
		}
	}
	b, _ := json.Marshal(res)
	return string(b)
}

// The CLI fails on a tool's error answer exactly as before the answer
// carried its code in "reason": the same exit code, code, message and hint,
// in both languages.
func TestToolError_SameAfterReasonContract(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	errs := []error{
		clerrors.Wrap(clerrors.CodeAPIUnreachable, "failed to reach IAM endpoint", "Check your network connection and try again", stderrors.New("dial")),
		clerrors.New(clerrors.CodeAuthFailed, "IAM rejected the credentials (status 401)", ""),
		clerrors.New(clerrors.CodeConfigInvalid, "S3 credentials not configured", ""),
		clerrors.New(clerrors.CodeNotFound, "image ubuntu not found", "evo image list ubuntu"),
		clerrors.New(clerrors.CodeVMNotFound, "resource not found", ""),
		clerrors.New(clerrors.CodeAPIError, "HTTP 500", ""),
		clerrors.New(clerrors.CodeSSHTimeout, "no answer", ""),
		fmt.Errorf("release floating IP: %w", clerrors.New(clerrors.CodeAuthExpired, "token rejected", "")),
		fmt.Errorf("create VM: %w", clerrors.New(clerrors.CodeAPIError, "HTTP 400 bad flavor", "")),
		fmt.Errorf("%s: %w", clerrors.CodeUsage, stderrors.New("'disk_id' is required")),
		stderrors.New("USAGE: vm_get requires vm_id or name"),
		stderrors.New("disk abc not found"),
		stderrors.New("HTTP: 502"),
		stderrors.New("API_ERROR: quota exceeded"),
	}
	for _, lang := range []string{"ru", "en"} {
		i18n.SetLang(lang)
		for _, err := range errs {
			before := toolError(oldErrResult(err), callHints("x"))
			after := toolError(tools.ErrResult(err), callHints("x"))
			var b, a *clerrors.CLIError
			if !stderrors.As(before, &b) || !stderrors.As(after, &a) {
				t.Fatalf("%v: not a CLIError", err)
			}
			// The one detail an error answer adds: "_untrusted" names its
			// error when the text is a service's (#1802).
			details := maps.Clone(a.Details)
			if u, _ := details["_untrusted"].([]any); len(u) == 1 && u[0] == "error" {
				delete(details, "_untrusted")
			}
			if a.Code != b.Code || a.Message != b.Message || a.Hint != b.Hint || a.Err != b.Err || len(details) != 0 || clerrors.ExitCode(after) != clerrors.ExitCode(before) {
				t.Errorf("%s %v:\n before %+v exit %d\n after  %+v exit %d", lang, err,
					*b, clerrors.ExitCode(before), *a, clerrors.ExitCode(after))
			}
		}
	}
}

// Релизный аудит, A-003: ошибка без кода, которая не пришла из API (нет
// HTTP-статуса, не сеть), — USAGE и код 1, а не API_ERROR и 5; ответ API
// без кода — по статусу.
func TestToolError_UncodedLocalErrorIsUsage(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code string
		exit int
	}{
		{stderrors.New("'disk_id' is required"), clerrors.CodeUsage, clerrors.ExitGeneral},
		{stderrors.New(`invalid job_id "../../etc/passwd" (want stk-<10 hex digits>, as stack_plan returns)`), clerrors.CodeUsage, clerrors.ExitGeneral},
		{stderrors.New("vpc API 422: bad cidr"), clerrors.CodeAPIError, clerrors.ExitAPI},
		{stderrors.New("dns API 404: zone"), clerrors.CodeNotFound, clerrors.ExitNotFound},
	} {
		err := toolError(tools.ErrResult(tt.err), callHints("x"))
		var ce *clerrors.CLIError
		if !stderrors.As(err, &ce) || ce.Code != tt.code || clerrors.ExitCode(err) != tt.exit {
			t.Errorf("%v: %v exit %d, want %s exit %d", tt.err, err, clerrors.ExitCode(err), tt.code, tt.exit)
		}
	}
}

func TestRedisIncompletePageErrorContract(t *testing.T) {
	for _, page := range []string{"null", `{"clusters":[null]}`} {
		t.Run(page, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			t.Setenv("EVO_LANG", "ru")
			defer i18n.SetLang(i18n.Lang())
			i18n.SetLang(i18n.RU)
			var pages []string
			old := w.rt.extra
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Host != "redis.api.cloud.ru" || r.URL.Path != "/v1/clusters" {
					return old(r)
				}
				token := r.URL.Query().Get("page_token")
				pages = append(pages, token)
				body := page
				if token == "" {
					body = `{"clusters":[{"id":"first"}],"next_page_token":"second"}`
				}
				return contractRT{status: 200, body: body}.RoundTrip(r)
			}
			newProcess()
			code, out, stderr := runCLI(t, "", "redis", "list", "--json")
			var result struct {
				Error struct{ Reason, Message string }
			}
			if err := json.Unmarshal([]byte(out+stderr), &result); err != nil {
				t.Fatal(err, out, stderr)
			}
			if code != 5 || result.Error.Reason != "API_ERROR" || !strings.Contains(result.Error.Message, "the list is not complete") || len(w.rt.takeWrites()) != 0 {
				t.Fatal(code, out, stderr)
			}
			pages = nil
			a := callStdioTool(t, "redis_create", map[string]any{"name": "cache", "cpu": 2, "ram": 4, "confirmed": true})
			if !a.isErr || parseObject(a.text)["reason"] != "API_ERROR" {
				t.Fatalf("confirmed Redis create: %s", a.text)
			}
			if !slices.Equal(pages, []string{"", "second"}) {
				t.Fatalf("confirmed create read page tokens %q", pages)
			}
			if writes := w.rt.takeWrites(); len(writes) != 0 {
				t.Fatalf("confirmed create sent %v", writes)
			}
		})
	}
}
