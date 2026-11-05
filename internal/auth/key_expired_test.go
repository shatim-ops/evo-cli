// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// R-26 (release plan 1.8, B-3): a key evo login made carries its term in
// the profile. When IAM refuses it after that term, the refusal says the
// key expired and a person is needed — a retry will not help — and marks
// key_expired; the reason and the exit code stay those of any refused key
// (AUTH_FAILED, 2; «релиз», вариант А). A key whose term is unknown or not
// passed gets the text of before.
func TestIAMProvider_ExpiredKeySaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer srv.Close()
	old := IAMEndpoint
	IAMEndpoint = srv.URL
	defer func() { IAMEndpoint = old }()

	past := time.Now().Add(-time.Hour)
	for name, c := range map[string]struct {
		expires time.Time
		expired bool
	}{
		"term passed":  {past, true},
		"term ahead":   {time.Now().Add(time.Hour), false},
		"term unknown": {time.Time{}, false},
	} {
		_, err := NewIAMProvider(Credentials{KeyID: "k", Secret: "s", ExpiresAt: c.expires}, srv.Client(), nil).Token(context.Background())
		var ce *clerrors.CLIError
		if !errors.As(err, &ce) || ce.Code != clerrors.CodeAuthFailed || clerrors.ExitCode(err) != clerrors.ExitAuth {
			t.Fatalf("%s: %v, want AUTH_FAILED, exit 2", name, err)
		}
		said := strings.Contains(ce.Message, c.expires.UTC().Format(time.RFC3339)) && strings.Contains(ce.Hint, "evo login")
		if said != c.expired || (ce.Details["key_expired"] == true) != c.expired {
			t.Errorf("%s: message %q, hint %q, details %v", name, ce.Message, ce.Hint, ce.Details)
		}
	}
}
