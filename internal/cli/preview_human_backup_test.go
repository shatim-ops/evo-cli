// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// The branches of the preview of backup_delete that check no dependent
// backups: the backup names no project, and the key may not list the
// backups of the project (401, 403; FB19I-24 (в), «Риск», 188, У4). The
// world of TestPreview_HumanShowsEveryValue takes neither; on it, each
// gives dependent_backups_checked: false with its warning, and the human
// preview of --dry-run shows every value of the details, as that test
// checks: at 40 columns and 200 alike.
func TestPreview_HumanShowsEveryValue_BackupDeleteUnchecked(t *testing.T) {
	w, fake := withJSONWorld(t)
	card := `{"id":"` + parityVM + `","name":"web-copy","state":"created","type":"full","size":20480,"created_at":"2026-11-12T10:00:00Z","tags":[],` +
		`"resource":{"resource_id":"` + parityVM + `","resource_type":"virtual_machine","resource_exists":true,"resource_state":{"name":"web"}}}`
	for name, answer := range map[string]func(r *http.Request) (*http.Response, error){
		"the backup names no project": func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet || r.URL.Path != "/api/v1/backups/"+parityVM {
				return nil, nil
			}
			return contractRT{status: 200, body: card}.RoundTrip(r)
		},
		"the list of backups 403": nil,
	} {
		parityBackupsRefused = nil
		if answer == nil {
			// The parity cloud lists the backups without HTTP.
			parityBackupsRefused = errors.New("compute API 403: refused")
			answer = func(*http.Request) (*http.Response, error) { return nil, nil }
		}
		world := w.rt.extra
		w.rt.extra = func(r *http.Request) (*http.Response, error) {
			if r.URL.Host == "compute.api.cloud.ru" {
				if resp, err := answer(r); resp != nil || err != nil {
					return resp, err
				}
			}
			return world(r)
		}
		run := func(extra ...string) string {
			fake.reset()
			newProcess()
			_, out, errOut := runCLI(t, "", append(extra, "backup", "delete", parityVM)...)
			forgetRun(w)
			return humanVolatile(out + errOut)
		}
		var env struct {
			Preview struct {
				Details map[string]any `json:"details"`
			} `json:"preview"`
		}
		jsonOut := run("--dry-run", "--json")
		if json.Unmarshal([]byte(jsonOut), &env) != nil || env.Preview.Details == nil {
			t.Fatalf("%s: no preview.details from --dry-run --json: %s", name, jsonOut)
		}
		will, _ := env.Preview.Details["will_delete"].(map[string]any)
		if will["dependent_backups_checked"] != false {
			t.Errorf("%s: want dependent_backups_checked false: %s", name, jsonOut)
		}
		for _, cols := range []string{"40", "200"} {
			t.Setenv("COLUMNS", cols)
			human := run("--dry-run")
			for _, miss := range humanMisses(env.Preview.Details, human) {
				if namedBySibling(env.Preview.Details, miss.path, miss.value, human) {
					continue
				}
				if _, ok := humanUnnamed["backup_delete "+miss.path]; ok {
					continue
				}
				t.Errorf("%s: %s = %s in the details, not in the human preview (COLUMNS=%s):\n%s", name, miss.path, miss.value, cols, human)
			}
		}
		w.rt.extra = world
	}
	parityBackupsRefused = nil
}
