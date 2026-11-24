// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

func TestHistoryRedisReadBudgetOnEachSurface(t *testing.T) {
	w, _ := withJSONWorld(t)
	newProcess()
	code, out, stderr := runCLI(t, "", "--json", "redis", "create", "own", "--cpu", "2", "--ram", "4", "--yes")
	if code != 0 || !strings.Contains(out, parityVM) {
		t.Fatal(out, stderr)
	}
	at := clock.Now().Add(time.Hour)
	clock.Now = func() time.Time { return at }
	old := w.rt.extra
	cards, pages := 0, 0
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Host != "redis.api.cloud.ru" {
			return old(r)
		}
		switch r.URL.Path {
		case "/v1/clusters/" + parityVM:
			cards++
			return contractRT{status: 200, body: fmt.Sprintf(`{"id":%q,"status":"ERASED"}`, parityVM)}.RoundTrip(r)
		case "/v1/clusters":
			pages++
			return contractRT{status: 200, body: fmt.Sprintf(`{"clusters":[{"id":%q}],"next_page_token":"%d"}`, parityVM, pages)}.RoundTrip(r)
		default:
			return old(r)
		}
	}
	for _, surface := range []string{"ru", "en", "json", "stdio"} {
		t.Run(surface, func(t *testing.T) {
			lang := "ru"
			if surface == "en" {
				lang = "en"
			}
			t.Setenv("EVO_LANG", lang)
			defer i18n.SetLang(i18n.Lang())
			i18n.SetLang(lang)
			cards, pages = 0, 0
			newProcess()
			var answer string
			switch surface {
			case "stdio":
				a := callStdioTool(t, "history", map[string]any{"check": true})
				if a.isErr || a.rpcErr != "" {
					t.Fatal(a)
				}
				answer = a.text
			default:
				args := []string{"history", "--check"}
				if surface == "json" {
					args = append(args, "--json")
				}
				code, o, e := runCLI(t, "", args...)
				if code != 0 {
					t.Fatal(code, o, e)
				}
				answer = o
			}
			if cards != 1 || pages != 49 {
				t.Fatalf("GET by ID=%d pages=%d", cards, pages)
			}
			if surface == "ru" || surface == "en" {
				want := "прочитано по id 1,"
				if surface == "en" {
					want = "1 read by id,"
				}
				if !strings.Contains(answer, want) {
					t.Errorf("want %q in %s", want, answer)
				}
				return
			}
			var v struct {
				Checked   struct{ Read int }
				CheckNote string `json:"check_note"`
				Live      []struct {
					Result string `json:"check_result"`
				}
			}
			if err := json.Unmarshal([]byte(answer), &v); err != nil {
				t.Fatal(err)
			}
			if v.Checked.Read != 1 || len(v.Live) != 1 || v.Live[0].Result != "read_failed" || !strings.Contains(v.CheckNote, "read by id") {
				t.Errorf("checked result: %+v", v)
			}
		})
	}
}
