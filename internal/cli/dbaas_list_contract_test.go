// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

func TestRedisIncompletePageErrorContract(t *testing.T) {
	for _, page := range []string{"null", `{"clusters":[null]}`} {
		t.Run(page, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			t.Setenv("EVO_LANG", "ru")
			defer i18n.SetLang(i18n.Lang())
			i18n.SetLang(i18n.RU)
			old := w.rt.extra
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Host != "redis.api.cloud.ru" || r.URL.Path != "/v1/clusters" {
					return old(r)
				}
				body := page
				if r.URL.Query().Get("page_token") == "" {
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
		})
	}
}
