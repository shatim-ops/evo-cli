package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Every error answer has a reason (a code of internal/errors) and the
// message without the leading "CODE: "; the hint of a CLIError in the chain
// comes too, so the CLI keeps the exit code and the hint, and an agent sees
// them.
func TestErrResult_ReasonAndHint(t *testing.T) {
	for _, tt := range []struct {
		err               error
		msg, reason, hint string
	}{
		{clierrors.Wrap(clierrors.CodeAPIUnreachable, "failed to reach IAM endpoint", "Check your network connection and try again", errors.New("dial")),
			"failed to reach IAM endpoint", "API_UNREACHABLE", "Check your network connection and try again"},
		// Context before the code stays, as in the CLI's own messages.
		{fmt.Errorf("release floating IP: %w", clierrors.New(clierrors.CodeAuthFailed, "denied", "")),
			"release floating IP: AUTH_FAILED: denied", "AUTH_FAILED", ""},
		// Релизный аудит, A-003: ошибка без кода и без ответа API — USAGE.
		{errors.New("plain"), "plain", "USAGE", ""},
		{errors.New("disk d1 not found"), "disk d1 not found", "NOT_FOUND", ""},
		// A code only in the text: moved to reason.
		{errors.New("USAGE: 'disk_id' is required"), "'disk_id' is required", "USAGE", ""},
		{fmt.Errorf("%s: %w", clierrors.CodeUsage, errors.New("bad")), "bad", "USAGE", ""},
		// An upper-case word that is not a code stays in the text.
		{errors.New("HTTP: 502"), "HTTP: 502", "API_ERROR", ""},
	} {
		var got map[string]any
		if err := json.Unmarshal([]byte(errResult(tt.err)), &got); err != nil {
			t.Fatal(err)
		}
		reason, _ := got["reason"].(string)
		hint, _ := got["hint"].(string)
		if got["status"] != "error" || got["error"] != tt.msg || reason != tt.reason || hint != tt.hint {
			t.Errorf("%v: %v", tt.err, got)
		}
		if _, ok := got["hint"]; !ok && tt.hint != "" || ok && tt.hint == "" {
			t.Errorf("%v: hint present = %v", tt.err, ok)
		}
	}
}

// usageResult puts USAGE in reason, the message as it is in error.
func TestUsageResult_Reason(t *testing.T) {
	var got map[string]any
	if err := json.Unmarshal([]byte(usageResult(errors.New("'name' is required"))), &got); err != nil {
		t.Fatal(err)
	}
	if got["reason"] != clierrors.CodeUsage || got["error"] != "'name' is required" || got["status"] != "error" {
		t.Errorf("usageResult: %v", got)
	}
}

// errAnswer keeps the fields of a hand-built answer and its own status, and
// finds "not found" in them as the CLI does (kubectl output).
func TestErrAnswer_Fields(t *testing.T) {
	var got map[string]any
	text := errAnswer(errors.New("exit status 1"), map[string]any{"output": `Error from server (NotFound): pods "x" not found`})
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "error" || got["reason"] != clierrors.CodeNotFound || got["error"] != "exit status 1" || got["output"] == nil {
		t.Errorf("kubectl answer: %v", got)
	}
	text = errAnswer(errors.New("VM v1 created but did not become active"), map[string]any{"status": "timeout", "vm_id": "v1"})
	got = nil
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	// Ошибка о созданной ВМ (vm_id в ответе) — VM_PROVISION_FAILED, как у
	// deploy_apply (релизный аудит, A-003; было API_ERROR).
	if got["status"] != "timeout" || got["reason"] != clierrors.CodeVMProvisionFailed || got["vm_id"] != "v1" {
		t.Errorf("timeout answer: %v", got)
	}
}
