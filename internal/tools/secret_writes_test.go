// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const secretWriteMark = "MARK-0f3c9e"

// A write that carries a secret (CLISpec.SecretFiles) shows of the
// service's answer only what evo knows to be safe, on both surfaces: a
// refusal names its class, the HTTP status and the request, not the
// service's text — which may quote the secret it was sent; a success names
// the declared fields of the object, not whatever else the service put in
// it (Астра, full audit of 14.11, findings 02 and 03).
func TestSecretWrites_ServiceTextAndFieldsStayIn(t *testing.T) {
	for tool, o := range secretWrites {
		refused := func(ctx context.Context, _ map[string]any) (string, bool) {
			err := &provision.StatusError{Status: 422, Body: `{"message":"bad value ` + secretWriteMark + `"}`}
			return failed(ctx, fmt.Errorf("write: %w", err), nil), true
		}
		c := &Caller{}
		text, isErr := outer(tool, map[string]any{}, refused)(WithCaller(context.Background(), c), map[string]any{})
		if !isErr || strings.Contains(text, secretWriteMark) {
			t.Errorf("%s: the refusal shows the service's text: %s", tool, text)
		}
		if c.Err == nil || strings.Contains(c.Err.Error(), secretWriteMark) || clierrors.Reason(c.Err) != clierrors.CodeAPIError {
			t.Errorf("%s: the CLI's error shows the service's text or lost its reason: %v", tool, c.Err)
		}
		if m := parseAnswer(t, text); m["reason"] != clierrors.CodeAPIError || !strings.Contains(text, "422") {
			t.Errorf("%s: the refusal lost its class or status: %s", tool, text)
		}
		ok := func(context.Context, map[string]any) (string, bool) {
			return toJSON(map[string]any{"status": "created", o.field: map[string]any{"id": "id-1", "extra": secretWriteMark,
				"nested": map[string]any{"x": secretWriteMark}}}), false
		}
		text, _ = outer(tool, map[string]any{}, ok)(context.Background(), map[string]any{})
		if strings.Contains(text, secretWriteMark) || !strings.Contains(text, "id-1") {
			t.Errorf("%s: the success shows a field not declared, or lost its id: %s", tool, text)
		}
	}
	// A refusal of evo itself, before any request, keeps its words.
	local := func(ctx context.Context, _ map[string]any) (string, bool) {
		return failed(ctx, usageErr("'path' and 'value' are required "+secretWriteMark, "нужны 'path' и 'value' "+secretWriteMark), nil), true
	}
	if text, _ := outer("secret_create", map[string]any{}, local)(context.Background(), map[string]any{}); !strings.Contains(text, secretWriteMark) {
		t.Errorf("evo's own refusal lost its words: %s", text)
	}
}

// Every tool with a secret input declares what of its answer is public,
// and only those: a new one fails here until it does.
func TestSecretWrites_EveryToolWithASecretInput(t *testing.T) {
	r := New(nil)
	for _, tl := range r.Tools() {
		_, declared := secretWrites[tl.Name]
		if (len(tl.CLI.SecretFiles) > 0) != declared {
			t.Errorf("%s: a secret input %v, declared in secretWrites %v", tl.Name, len(tl.CLI.SecretFiles) > 0, declared)
		}
	}
}
