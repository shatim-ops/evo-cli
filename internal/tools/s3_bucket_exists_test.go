// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// D101 answers already_exists for the profile's own bucket; it adds no
// refusal for a key that may not list the buckets (403 on the list): such
// a call goes on as before — the question, then the create («релиз»,
// 12.11, on conclusion 130 of «Риск»). And a bucket of another account
// that answers a HEAD (public read) is not «yours»: the create fails.
func TestS3BucketCreate_ListRefusedGoesOnAndForeignIsNotYours(t *testing.T) {
	for _, c := range []struct {
		name, putCode string
		wantStatus    string
	}{
		{name: "the list is refused, the create passes", wantStatus: "created"},
		{name: "the list is refused, the name is of another account", putCode: "BucketAlreadyExists", wantStatus: "error"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var reqs []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				reqs = append(reqs, r.Method+" "+r.URL.Path)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/xml")
				switch {
				case r.Method == http.MethodGet && strings.Trim(r.URL.Path, "/") == "":
					w.WriteHeader(http.StatusForbidden)
					_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`)
				case r.Method == http.MethodPut && c.putCode != "":
					w.WriteHeader(http.StatusConflict)
					_, _ = io.WriteString(w, `<Error><Code>`+c.putCode+`</Code><Message>taken</Message></Error>`)
				}
			}))
			defer srv.Close()
			r := New(func(map[string]any) (*engine.Engine, error) {
				var buf bytes.Buffer
				return &engine.Engine{
					Config: config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s",
						S3: config.S3Config{Endpoint: srv.URL, AccessKey: "a", SecretKey: "s", Region: "ru-central-1"}},
					Auth:    staticToken{},
					Printer: output.NewWithWriter(&buf, output.ModeText, false),
				}, nil
			})
			r.SetStateDir(t.TempDir())
			m, text, _ := callNamed(t, r, "s3_bucket_create", map[string]any{"bucket": "new-bucket"})
			if m["status"] != "confirmation_required" {
				t.Fatalf("without consent: %s, want the question", text)
			}
			mu.Lock()
			for _, q := range reqs {
				if !strings.HasPrefix(q, "GET ") {
					t.Errorf("the preview sent %s", q)
				}
			}
			mu.Unlock()
			m, text, _ = callNamed(t, r, "s3_bucket_create", map[string]any{"bucket": "new-bucket", "confirmed": true})
			if m["status"] != c.wantStatus || strings.Contains(text, "already_exists") {
				t.Errorf("with consent: %s, want status %s and no already_exists", text, c.wantStatus)
			}
		})
	}
}
