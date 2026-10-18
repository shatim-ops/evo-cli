// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// C2: ответ инструмента с кодом, у которого exit 1, сохраняет код и reason.
func TestQAr3_ToolErrorKeepsAPIUnreachable(t *testing.T) {
	err := toolError("vm_list", `{"error":"API_UNREACHABLE: IAM unreachable","status":"error"}`)
	var ce *clerrors.CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("not a CLIError: %v", err)
	}
	if ce.Code != clerrors.CodeAPIUnreachable || clerrors.ExitCode(err) != 1 {
		t.Fatalf("code=%s exit=%d, want API_UNREACHABLE/1 (docs/exit-codes.md)", ce.Code, clerrors.ExitCode(err))
	}
}

// C2 in Russian: the answer of a tool (errResult: error, reason, hint) gives
// the error whoami would give — code, exit 1, the hint of the tool.
func TestToolError_KeepsCodeAndHint_Russian(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang("ru")
	text := `{"status":"error","error":"API_UNREACHABLE: IAM недоступен","reason":"API_UNREACHABLE","hint":"Проверьте подключение к сети и повторите"}`
	err := toolError("vm_list", text)
	var ce *clerrors.CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("not a CLIError: %v", err)
	}
	if ce.Code != clerrors.CodeAPIUnreachable || clerrors.ExitCode(err) != clerrors.ExitGeneral ||
		clerrors.Reason(err) != clerrors.CodeAPIUnreachable {
		t.Errorf("code=%s exit=%d reason=%s", ce.Code, clerrors.ExitCode(err), clerrors.Reason(err))
	}
	if ce.Message != "vm_list: IAM недоступен" || ce.Hint != "Проверьте подключение к сети и повторите" {
		t.Errorf("message %q, hint %q", ce.Message, ce.Hint)
	}
}

// C9: the hint of the tool wins over the generic one; without a hint the
// generic one is still given.
func TestToolError_HintOfTheTool(t *testing.T) {
	for _, tt := range []struct{ text, code, hint string }{
		{`{"error":"CONFIG_INVALID: bad yaml","hint":"Fix the YAML syntax"}`, clerrors.CodeConfigInvalid, "Fix the YAML syntax"},
		{`{"error":"AUTH_FAILED: IAM rejected the credentials (status 401)","hint":"Verify the key ID and secret"}`, clerrors.CodeAuthFailed, "Verify the key ID and secret"},
		{`{"error":"AUTH_FAILED: bad key"}`, clerrors.CodeAuthFailed, "Run 'evo configure', or set CLOUDRU_KEY_ID and CLOUDRU_SECRET."},
		{`{"error":"USAGE: 'disk_id' is required"}`, clerrors.CodeUsage, "Check the arguments: 'evo tools describe x'."},
		// A code deeper in the message: reason carries it.
		{`{"error":"release floating IP: AUTH_EXPIRED: token rejected","reason":"AUTH_EXPIRED"}`, clerrors.CodeAuthExpired, "Run 'evo configure', or set CLOUDRU_KEY_ID and CLOUDRU_SECRET."},
		{`{"error":"SSH_TIMEOUT: no answer"}`, clerrors.CodeSSHTimeout, ""},
		// Not a code: an upper-case word only.
		{`{"error":"HTTP: 500"}`, clerrors.CodeAPIError, ""},
	} {
		var ce *clerrors.CLIError
		if !errors.As(toolError("x", tt.text), &ce) || ce.Code != tt.code || ce.Hint != tt.hint {
			t.Errorf("%s: got %+v, want %s %q", tt.text, ce, tt.code, tt.hint)
		}
	}
}

// C9 end to end: a key IAM rejects fails evo vm list as it fails whoami —
// AUTH_FAILED, exit 2, the hint about the key, in Russian.
func TestVMList_RejectedKey_Russian(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang("ru")
	fc := setupCLI(t)
	saveProfile(t, "default", "p-1")
	fc.iamStatus = http.StatusUnauthorized
	code, _, stderr := runCLI(t, "", "--json", "vm", "list")
	if code != clerrors.ExitAuth {
		t.Fatalf("exit %d, want %d: %s", code, clerrors.ExitAuth, stderr)
	}
	var got struct {
		Error struct {
			Reason, Message, Hint string
		} `json:"error"`
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &got); err != nil {
		t.Fatalf("stderr: %v\n%s", err, stderr)
	}
	if got.Error.Reason != clerrors.CodeAuthFailed ||
		!strings.Contains(got.Error.Message, "IAM отклонил ключ доступа (статус 401)") ||
		!strings.HasPrefix(got.Error.Hint, "Проверьте Key ID и секрет") {
		t.Errorf("error %+v", got.Error)
	}
}
