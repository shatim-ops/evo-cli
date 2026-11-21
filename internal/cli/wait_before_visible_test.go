// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

func TestWaitBeforeVisibleOnEachSurface(t *testing.T) {
	for _, surface := range []string{"ru", "en", "json", "stdio"} {
		t.Run(surface, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			lang := i18n.RU
			if surface == "en" {
				lang = i18n.EN
			}
			t.Setenv("EVO_LANG", lang)
			defer i18n.SetLang(i18n.Lang())
			i18n.SetLang(lang)
			if surface == "stdio" {
				// Start the MCP command in the Russian environment before serving
				// the call on the transport backed by the parity world's engine.
				if code, _, e := runCLI(t, "", "mcp"); code != 0 {
					t.Fatal(e)
				}
			}
			start := clock.Now()
			var ticks atomic.Int64
			clock.Now = func() time.Time { return start.Add(time.Duration(ticks.Load()) * 10 * time.Second) }
			oldFactory := toolEngine
			toolEngine = func(args map[string]any) (*engine.Engine, error) {
				e, err := oldFactory(args)
				if err == nil {
					e.Timings.VMPollTimeout = 10 * time.Second
					e.Timings.VMProgressInterval = 30 * time.Second
				}
				return e, err
			}
			oldRead := w.rt.extra
			var posted atomic.Bool
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "compute.api.cloud.ru" || r.URL.Path != "/api/v1/nat-gateways" {
					return oldRead(r)
				}
				body := `{"items":[],"total":0}`
				switch r.Method {
				case "POST":
					posted.Store(true)
					body = fmt.Sprintf(`{"id":%q,"name":"nat-new","status":"CREATING"}`, parityVM)
				case "GET":
					if posted.Load() && ticks.Add(1) == 8 {
						body = fmt.Sprintf(`{"items":[{"id":%q,"name":"nat-new","status":"CREATED"}],"total":1}`, parityVM)
					}
				default:
					t.Fatalf("unexpected method %s", r.Method)
				}
				return contractRT{status: 200, body: body}.RoundTrip(r)
			}
			var messages string
			switch surface {
			case "stdio":
				args := map[string]any{"name": "nat-new", "availability_zone_id": pvZone, "confirmed": true}
				body := roRequest("tools/call", map[string]any{"name": "snat_create", "arguments": args, "_meta": map[string]any{"progressToken": "watch"}})
				var result roAnswer
				for _, line := range strings.Split(strings.TrimSpace(string(runStdio(t, parityServers(t).stdio, body))), "\n") {
					var event struct {
						Method string
						Params struct{ Message string }
					}
					if err := json.Unmarshal([]byte(line), &event); err != nil {
						t.Fatal(err)
					}
					if event.Method == "notifications/progress" {
						messages += event.Params.Message + "\n"
					} else {
						result, _ = roDecode(t, []byte(line))
					}
				}
				if result.isErr || result.rpcErr != "" || !strings.Contains(result.text, `"created"`) {
					t.Fatal(result)
				}
			default:
				args := []string{"snat", "create", "nat-new", "--availability-zone-id", pvZone, "--yes"}
				if surface == "json" {
					args = append(args, "--json")
				}
				code, out, stderr := runCLI(t, "", args...)
				if code != 0 {
					t.Fatal(code, out, stderr)
				}
				messages = stderr
				if surface == "json" {
					var answer struct{ Status, Note string }
					if err := json.Unmarshal([]byte(out), &answer); err != nil || answer.Status != "created" || !strings.Contains(answer.Note, "SNAT gateway") {
						t.Fatal(out, err)
					}
					if strings.Contains(out+stderr, "so far") || strings.Contains(out+stderr, "прошло") {
						t.Fatal(out, stderr)
					}
				}
			}
			if ticks.Load() != 8 {
				t.Fatalf("GET after POST=%d", ticks.Load())
			}
			if surface == "json" {
				return
			}
			a, b, phrase := "30s so far", "1m0s so far", "not listed"
			if surface == "ru" {
				a, b, phrase = "прошло 30с", "прошло 1м0с", "списке ресурса нет"
			}
			if strings.Count(messages, phrase) != 2 || !strings.Contains(messages, a) || !strings.Contains(messages, b) {
				t.Fatalf("progress: %s", messages)
			}
		})
	}
}
