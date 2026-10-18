// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The spec names the token fields accessToken/expiresIn; an answer in that
// form, or one without a lifetime, still gives a usable token that is not
// "expired at once" (api-contract §4.12).
func TestIAMProvider_CamelCaseAndMissingTTL(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"camel":  {"accessToken": "tok-camel", "expiresIn": 3600},
		"no ttl": {"access_token": "tok-nottl"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(body)
		}))
		old := IAMEndpoint
		IAMEndpoint = srv.URL
		tok, err := NewIAMProvider(testCreds, srv.Client(), nil).Token(context.Background())
		IAMEndpoint = old
		srv.Close()
		if err != nil || tok.Value == "" {
			t.Errorf("%s: token %+v, %v", name, tok, err)
			continue
		}
		if tok.TTL() < 30*time.Minute {
			t.Errorf("%s: TTL %s, want a usable lifetime", name, tok.TTL())
		}
	}
}
