// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"errors"
	"net/http"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The compute API errors speak the language of the CLI: Russian by default,
// English with EVO_LANG=en (the rest of the package runs in English). Codes
// and HTTP statuses stay as they are.
func TestAdapterErrors_Russian(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())

	tests := []struct {
		status        int
		code          string
		msgRU, hintRU string
		msgEN, hintEN string
		call          func(a *CloudRuAdapter) error
	}{
		{
			status: http.StatusForbidden, code: clierrors.CodeAuthFailed,
			msgRU:  "доступ запрещён (403): GET /api/v1/availability-zones: {\"code\":\"forbidden\"}",
			hintRU: "У сервисного аккаунта нет роли для этой операции в проекте.",
			msgEN:  "access denied (403) for GET /api/v1/availability-zones: {\"code\":\"forbidden\"}",
			hintEN: "The service account lacks a role for this operation in the project.",
			call: func(a *CloudRuAdapter) error {
				_, err := a.ListAvailabilityZones(context.Background())
				return err
			},
		},
		{
			status: http.StatusTooManyRequests, code: clierrors.CodeAPIRateLimited,
			msgRU:  "превышен лимит запросов к API, повторы не помогли",
			hintRU: "Подождите немного и повторите",
			msgEN:  "API rate limit exceeded after retries",
			hintEN: "Wait a moment and try again",
			call: func(a *CloudRuAdapter) error {
				_, err := a.GetVM(context.Background(), "vm-1")
				return err
			},
		},
		{
			status: http.StatusServiceUnavailable, code: clierrors.CodeAPIError,
			msgRU:  "API вернул статус 503 после 3 повторов: {\"code\":\"forbidden\"}",
			hintRU: "Проверьте страницу статуса Cloud.ru или повторите позже",
			msgEN:  "API returned status 503 after 3 retries: {\"code\":\"forbidden\"}",
			hintEN: "Check Cloud.ru status page or try again later",
			call: func(a *CloudRuAdapter) error {
				_, err := a.GetVM(context.Background(), "vm-1")
				return err
			},
		},
		{
			status: http.StatusUnauthorized, code: clierrors.CodeAuthExpired,
			msgRU:  "API Cloud.ru отклонил токен (401)",
			hintRU: "Проверьте ключ доступа командой 'evo doctor' или заново выполните 'evo configure'.",
			msgEN:  "Cloud.ru API rejected the token (401)",
			hintEN: "Check the access key with 'evo doctor', or run 'evo configure' again.",
			call: func(a *CloudRuAdapter) error {
				_, err := a.GetVM(context.Background(), "vm-1")
				return err
			},
		},
	}
	for _, tt := range tests {
		srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tt.status)
			_, _ = w.Write([]byte(`{"code":"forbidden"}`))
		})
		for _, lang := range []string{"ru", "en"} {
			i18n.SetLang(lang)
			err := tt.call(adapter)
			var ce *clierrors.CLIError
			if !errors.As(err, &ce) {
				t.Fatalf("%d/%s: not a CLIError: %v", tt.status, lang, err)
			}
			msg, hint := tt.msgRU, tt.hintRU
			if lang == "en" {
				msg, hint = tt.msgEN, tt.hintEN
			}
			if ce.Code != tt.code || ce.Message != msg || ce.Hint != hint {
				t.Errorf("%d/%s:\n got  %s | %q | %q\n want %s | %q | %q", tt.status, lang, ce.Code, ce.Message, ce.Hint, tt.code, msg, hint)
			}
		}
		srv.Close()
	}
}
