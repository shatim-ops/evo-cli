// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// A cluster of the same name is visible to this key. Only a read in the
// appropriate scope may settle the interrupted create (R-86, condition 6).
func TestContract_DoctorCreateNameAndKey(t *testing.T) {
	for _, kind := range []string{"another key", "same key", "legacy without key", "without project", "empty identity", "without key or project"} {
		t.Run(kind, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			eng, err := toolEngine(nil)
			if err != nil {
				t.Fatal(err)
			}
			key := eng.Maker()
			if key == nil {
				t.Fatal("fixture needs an identified key")
			}
			project := eng.Config.ProjectID
			switch kind {
			case "another key":
				key = &state.Maker{KeyID: "other-key"}
			case "without project":
				project = ""
			case "empty identity":
				key = &state.Maker{}
			case "without key or project":
				key = nil
				project = ""
			case "legacy without key":
				key = nil
			}
			dir := intent.Dir(w.dir)
			rec, err := intent.Open(dir, intent.Intent{Tool: "redis_create", Name: "same-name", ProjectID: project, Key: key})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, rec.ID+".json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			reads := 0
			previous := w.rt.extra
			w.rt.extra = func(req *http.Request) (*http.Response, error) {
				if req.Method == "POST" && req.URL.Host == "iam.api.cloud.ru" && req.URL.Path == "/api/v1/auth/token" {
					return contractRT{status: 200, body: `{"access_token":"fake-token","expires_in":3600}`}.RoundTrip(req)
				}
				if req.Method == "GET" && req.URL.Host == "redis.api.cloud.ru" && req.URL.Path == "/v1/clusters" {
					reads++
					return contractRT{status: 200, body: `{"clusters":[{"id":"listed-id","name":"same-name"}]}`}.RoundTrip(req)
				}
				return previous(req)
			}
			code, out, stderr := runCLI(t, "", "doctor", "--json")
			if code != 0 {
				t.Fatal(code, stderr)
			}
			var answer struct{ Checks []tools.DoctorCheck }
			if err := json.Unmarshal([]byte(out), &answer); err != nil {
				t.Fatal(err)
			}
			var pending []tools.DoctorCheck
			for _, check := range answer.Checks {
				if check.Name == "pending operation" {
					pending = append(pending, check)
				}
			}
			if len(pending) != 1 {
				t.Fatalf("doctor returned %d individual checks", len(pending))
			}
			if kind == "without key or project" {
				defer i18n.SetLang(i18n.Lang())
				t.Setenv("EVO_LANG", "ru")
				i18n.SetLang(i18n.RU)
				exit, human, errOut := runCLI(t, "", "doctor")
				if exit != 0 || !strings.Contains(human, "Не удалось подтвердить владельца и проект") {
					t.Fatal(exit, human, errOut)
				}
				answer := callStdioTool(t, "doctor", map[string]any{})
				if answer.isErr || !strings.Contains(answer.text, "Cannot verify the owner and project") {
					t.Fatal(answer.text)
				}
			}
			after, err := os.ReadFile(path)
			if kind != "same key" {
				why := "Cannot verify"
				if kind == "another key" {
					why = "sent with another key"
				}
				if err != nil || string(before) != string(after) || reads != 0 || pending[0].Status != tools.CheckWarn || !strings.Contains(pending[0].Detail, why) {
					t.Fatalf("foreign evidence: read error %v, cluster lists %d, check %+v", err, reads, pending[0])
				}
			} else if !os.IsNotExist(err) || reads != 1 || pending[0].Status != tools.CheckOK || !strings.Contains(pending[0].Detail, "listed-id") {
				t.Fatalf("known outcome: read error %v, cluster lists %d, check %+v", err, reads, pending[0])
			}
		})
	}
}
