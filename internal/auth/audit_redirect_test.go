// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// A 307/308 from the IAM endpoint must not make the client re-POST the key
// secret to the Location: the exchange fails instead.
func TestIAMProvider_DoesNotFollowRedirectsWithTheSecret(t *testing.T) {
	var leaked atomic.Bool
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "s3cr3t") {
			leaked.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok","expires_in":3600}`)
	}))
	t.Cleanup(sink.Close)

	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		leaked.Store(false)
		iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, sink.URL+"/token", status)
		}))
		old := IAMEndpoint
		IAMEndpoint = iam.URL
		// The shared CLI client follows redirects by default: that is the
		// behaviour the provider has to override.
		p := NewIAMProvider(Credentials{KeyID: "k", Secret: "s3cr3t"}, &http.Client{}, nil)
		_, err := p.Token(context.Background())
		IAMEndpoint = old
		iam.Close()

		if leaked.Load() {
			t.Fatalf("status %d: the key secret was POSTed to the redirect target", status)
		}
		if err == nil {
			t.Fatalf("status %d: exchange succeeded through a redirect", status)
		}
		if code := clerrors.ExitCode(err); code != clerrors.ExitAPI {
			t.Errorf("status %d: exit code %d (%v), want %d", status, code, err, clerrors.ExitAPI)
		}
	}
}
