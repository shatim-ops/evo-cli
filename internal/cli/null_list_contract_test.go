// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// A list is also the source of a paid create's check for a free name.
// A corrupt last page must reach both callers before they get a preview.
func TestContract_IncompletePostgresCatalog(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	setupCLI(t)
	saveProfile(t, "default", allowedProject)
	prev := httpretry.DefaultBase
	var gets, other atomic.Int32
	httpretry.DefaultBase = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Hostname(), ".cloud.ru") {
			return prev.RoundTrip(r)
		}
		w := httptest.NewRecorder()
		switch {
		case r.Method != http.MethodGet || r.URL.Hostname() != "postgresql.api.cloud.ru" || r.URL.Path != "/v1/clusters":
			other.Add(1)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		case r.URL.Query().Get("page_token") == "":
			gets.Add(1)
			io.WriteString(w, `{"clusters":[{"id":"one","name":"one"}],"next_page_token":"next"}`)
		default:
			gets.Add(1)
			io.WriteString(w, "null")
		}
		return w.Result(), nil
	})
	t.Cleanup(func() { httpretry.DefaultBase = prev })
	for _, op := range []string{"list", "create"} {
		params := map[string]any{}
		args := []string{"pg", op}
		if op == "create" {
			params["name"] = "new-name"
			args = append(args, "new-name")
		}
		for _, c := range []struct {
			name, lang, fragment string
			json                 bool
		}{{"human-RU", "ru", "список не полон", false}, {"human-EN", "en", "the list is not complete", false}, {"json-RU", "ru", "the list is not complete", true}} {
			t.Run(op+"/"+c.name, func(t *testing.T) {
				t.Setenv("EVO_LANG", c.lang)
				if c.lang == "ru" {
					i18n.SetLang(i18n.RU)
				} else {
					i18n.SetLang(i18n.EN)
				}
				before := gets.Load()
				argv := append([]string(nil), args...)
				if c.json {
					argv = append(argv, "--json")
				}
				code, out, stderr := runCLI(t, "", argv...)
				if code != 5 || !strings.Contains(out+stderr, c.fragment) || strings.Contains(out+stderr, "will_create") || gets.Load()-before != 2 {
					t.Errorf("exit=%d GET=%d stdout=%s stderr=%s", code, gets.Load()-before, out, stderr)
				}
			})
		}
		t.Setenv("EVO_LANG", "ru")
		i18n.SetLang(i18n.RU)
		for name, call := range mcpTransports(t, nil) {
			t.Run(op+"/"+name, func(t *testing.T) {
				before := gets.Load()
				var answer roAnswer
				if name == "stdio" {
					request := roRequest("tools/call", map[string]any{"name": "pg_" + op, "arguments": params})
					code, out, stderr := runCLI(t, stdioInit+string(request)+"\n", "mcp")
					if code != 0 {
						t.Fatalf("evo mcp exit %d: %s", code, stderr)
					}
					_, reply, ok := strings.Cut(out, "\n")
					if !ok {
						t.Fatalf("missing stdio reply: %s", out)
					}
					answer, _ = roDecode(t, []byte(reply))
				} else {
					// The HTTP fixture instantiates the handler below runMCP,
					// which selects English before starting either transport.
					defer i18n.SetLang(i18n.Lang())
					i18n.SetLang(i18n.EN)
					answer = call("pg_"+op, params)
				}
				if !answer.isErr || parseObject(answer.text)["reason"] != "API_ERROR" || !strings.Contains(answer.text, "the list is not complete") || strings.Contains(answer.text, "will_create") || gets.Load()-before != 2 {
					t.Errorf("GET=%d answer=%+v", gets.Load()-before, answer)
				}
			})
		}
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range intent.Dirs(root) {
		if n := len(intent.Pending(dir)); n != 0 {
			t.Errorf("incomplete catalog left %d pending records", n)
		}
	}
	if n := other.Load(); n != 0 {
		t.Errorf("%d unexpected requests", n)
	}
}
