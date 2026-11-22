// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// Keep line boundaries and ignore the padding that aligns table cells.
func historyTableText(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}

// vm history shows Compute Tasks via the registered command's generic tree view.
func TestVMHistoryRender(t *testing.T) {
	const first = "00000000-0000-4000-8000-000000000002"
	const second = "00000000-0000-4000-8000-000000000001"
	const started = "2026-09-20T10:00:00Z"
	const changed = "2026-09-20T10:02:00Z"
	task := func(id, action, extra string) string {
		return fmt.Sprintf(`{"entity_type":"vm","entity_id":%q,"id":%q,"name":%q,"status":"complete","created_time":%q%s}`, parityVM, id, action, started, extra)
	}
	complete := task(first, "Start VM", `,"modified_time":"`+changed+`"`)
	projected := []any{
		map[string]any{"id": first, "action": "Start VM", "state": "complete", "created_time": started, "modified_time": changed},
		map[string]any{"id": second, "action": "Stop VM", "state": "complete", "created_time": started},
	}
	for _, tc := range []struct {
		name        string
		items       []string
		count, code int
	}{
		{"two tasks", []string{complete, task(second, "Stop VM", "")}, 2, 0},
		{"empty", nil, 0, 0},
		{"incomplete task", []string{complete, task(second, "", "")}, 1, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world, _ := withJSONWorld(t)
			body := fmt.Sprintf(`{"items":[%s],"total":%d}`, strings.Join(tc.items, ","), len(tc.items))
			world.rt.extra = func(r *http.Request) (*http.Response, error) {
				q := r.URL.Query()
				if r.Method != http.MethodGet || r.URL.Host != "compute.api.cloud.ru" || r.URL.Path != "/api/v1/tasks" ||
					q.Get("entity_id") != parityVM || q.Get("entity_type") != "vm" || q.Get("project_id") != pvProject {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL)
				}
				return contractRT{status: 200, body: body}.RoundTrip(r)
			}
			reply := callStdioTool(t, "vm_history", map[string]any{"vm_id": parityVM})
			machine := parseObject(reply.text)
			if reply.rpcErr != "" || reply.isErr != (tc.code != 0) || !reflect.DeepEqual(machine["tasks"], projected[:tc.count]) {
				t.Fatalf("stdio: rpc=%q isError=%t answer=%s", reply.rpcErr, reply.isErr, reply.text)
			}
			var firstJSON string
			for _, loc := range []struct{ lang, fields, no, columns, invalid string }{
				{"en", "FIELD VALUE", "false", "ID STATE ACTION CREATED_TIME MODIFIED_TIME", "Error: Compute Tasks returned an incomplete, unrelated or repeated task."},
				{"ru", "ПОЛЕ ЗНАЧЕНИЕ", "нет", "ID Состояние ACTION Создана MODIFIED_TIME", "Ошибка: Compute вернул неполную, постороннюю или повторную запись задачи."},
			} {
				t.Run(loc.lang, func(t *testing.T) {
					defer i18n.SetLang(i18n.Lang())
					t.Setenv("EVO_LANG", loc.lang)
					i18n.SetLang(loc.lang)
					want := fmt.Sprintf("%s\ncount %d\npartial %s\nsource Compute Tasks\n", loc.fields, tc.count, loc.no)
					if tc.count == 0 {
						want += "tasks"
					} else {
						want += "\nTASKS:\n" + loc.columns + "\n" + first + " complete Start VM " + started + " " + changed + "\n" + second + " complete Stop VM " + started
					}
					wantErr := ""
					if tc.code != 0 {
						want = ""
						wantErr = loc.invalid + "\n"
					}
					var prior string
					for n := 0; n < 2; n++ {
						newProcess()
						code, out, stderr := runCLI(t, "", "vm", "history", parityVM)
						if code != tc.code || historyTableText(out) != want || stderr != wantErr || (n > 0 && prior != out) {
							t.Fatalf("run %d: exit=%d stdout=%q stderr=%q", n, code, out, stderr)
						}
						prior = out
					}
					code, out, stderr := runCLI(t, "", "vm", "history", parityVM, "--json")
					answer := parseObject(out)
					wire := out + stderr
					if tc.code != 0 {
						envelope, ok := errorEnvelope(stderr)
						if !ok || out != "" || envelope.Code != tc.code || envelope.Reason != "API_ERROR" {
							t.Fatal(wire)
						}
						answer = envelope.Details
					} else if stderr != "" {
						t.Fatal(stderr)
					}
					if code != tc.code || answer["count"] != float64(tc.count) || answer["source"] != "Compute Tasks" || answer["partial"] != (tc.code != 0) || !reflect.DeepEqual(answer["tasks"], projected[:tc.count]) {
						t.Fatalf("exit=%d JSON=%s", code, wire)
					}
					if !reflect.DeepEqual(answer["_untrusted"], machine["_untrusted"]) || (tc.code == 0 && !reflect.DeepEqual(answer, machine)) {
						t.Fatalf("CLI JSON %s differs from stdio %s", wire, reply.text)
					}
					if firstJSON != "" && wire != firstJSON {
						t.Fatalf("JSON changed with locale: %s", wire)
					}
					firstJSON = wire
				})
			}
		})
	}
}

// The root history command renders the local journal, including check results.
func TestHistoryJournalRender(t *testing.T) {
	t.Chdir(t.TempDir())
	reg := newRegistry()
	const checked = `{
        "since":"day",
        "lines":[{"name":"chosen","target":"ignored","tool":"vm_start","project_id":"project","zone":"zone","surface":"cli","intent_id":"call"}],
        "live":[{"target":"fallback","check":"vm_get","check_result":"read_failed"}],
        "checked":{"read":1,"would_close":1,"not_checked":{"a":1,"b":2}},
        "would_close":[{"tool":"vm_delete","resource_id":"gone"}],"check_note":"check-note",
        "live_truncated":true,"truncated":true,"retired_through":"2026-09","skipped":4,"hidden_without_project":5,"note":"end-note"
    }`
	for _, tc := range []struct {
		name, body string
		en, ru     []string
	}{
		{"empty", `{"since":"day","lines":[],"live":[]}`,
			[]string{"Since: day", "No changes in this period.", "Live: nothing"},
			[]string{"С: day", "Изменений за этот период нет.", "Живое: ничего"}},
		{"unchecked", `{"live":[{"tool":"vm_start","target":"target-only","check":"custom_read"}]}`,
			[]string{"Check with", "vm_start target-only custom_read"},
			[]string{"Проверить", "vm_start target-only custom_read"}},
		{"checked", checked,
			[]string{
				"Since: day", "vm_start chosen project zone cli call", "Read by id", "fallback evo vm get read_failed",
				"Checked: 1 read by id, 1 not seen by this key, 3 not read.", "vm_delete gone", "check-note",
				"Only the last live lines", "Only the last lines", "Lines of 2026-09", "4 lines of the journal",
				"5 calls with no project", "end-note",
			},
			[]string{
				"С: day", "vm_start chosen project zone cli call", "Чтение по id", "fallback evo vm get read_failed",
				"Проверено: прочитано по id 1, этот ключ не видит 1, не прочитано 3.", "vm_delete gone", "check-note",
				"Показаны только последние живые строки", "Показаны только последние строки", "Строки за 2026-09",
				"Строк журнала не в форме evo, пропущено: 4", "Вызовов без проекта, не показанных на этом сервере: 5", "end-note",
			}},
	} {
		for _, lang := range []string{i18n.EN, i18n.RU} {
			t.Run(tc.name+"/"+lang, func(t *testing.T) {
				defer i18n.SetLang(i18n.Lang())
				i18n.SetLang(lang)
				var b bytes.Buffer
				if err := renderHistory(output.NewWithWriter(&b, output.ModeText, false), &b, reg, tc.body); err != nil {
					t.Fatal(err)
				}
				want := tc.en
				if lang == i18n.RU {
					want = tc.ru
				}
				rest := historyTableText(b.String())
				for _, s := range want {
					_, after, ok := strings.Cut(rest, s)
					if !ok {
						t.Fatalf("missing or misplaced %q in:\n%s", s, b.String())
					}
					rest = after
				}
				if strings.Contains(b.String(), "ignored") {
					t.Fatal("both name and target shown")
				}
			})
		}
	}
	var b bytes.Buffer
	if err := renderHistory(output.NewWithWriter(&b, output.ModeText, false), &b, reg, "{"); err == nil || b.Len() != 0 {
		t.Fatalf("invalid journal: err=%v output=%q", err, b.String())
	}
}
