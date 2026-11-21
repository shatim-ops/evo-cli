// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Both directories contribute. Reading the summary must preserve the original
// bytes, including fields a newer or older writer may have left in a record.
func TestContract_DoctorUnknownIntentQueue(t *testing.T) {
	w, _ := withJSONWorld(t)
	previous := w.rt.extra
	w.rt.extra = func(req *http.Request) (*http.Response, error) {
		if req.Method == "POST" && req.URL.Host == "iam.api.cloud.ru" && req.URL.Path == "/api/v1/auth/token" {
			return contractRT{status: 200, body: `{"access_token":"fake-token","expires_in":3600}`}.RoundTrip(req)
		}
		if req.Method == "GET" && req.URL.Host == "redis.api.cloud.ru" && req.URL.Path == "/v1/clusters" {
			return contractRT{status: 200, body: `{"clusters":[{"id":"known-id","name":"known"}]}`}.RoundTrip(req)
		}
		return previous(req)
	}
	defer i18n.SetLang(i18n.Lang())
	dir := intent.Dir(w.dir)
	oldTime := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	open := func(in intent.Intent) intent.Intent {
		t.Helper()
		in.StartedAt = oldTime
		rec, err := intent.Open(dir, in)
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	configDir := intent.ConfigDir()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(configDir, "0123456789abcdef.json")
	legacy := `{"id":"0123456789abcdef","tool":"vm_reboot","name":"legacy","started_at":"2001-01-01T00:00:00Z"}`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	foreign := open(intent.Intent{Tool: "disk_delete", Target: "disk_id=foreign-disk", Key: &state.Maker{KeyID: "other-key"}, Sent: true, Ended: true})
	foreignPath := filepath.Join(dir, foreign.ID+".json")
	foreignBytes := read(foreignPath)
	live := open(intent.Intent{Tool: "vm_reboot", PID: os.Getpid()})
	t.Cleanup(func() { intent.Close(live, history.Outcome{Result: history.Found}) })
	open(intent.Intent{Tool: "vm_reboot", Held: true})
	open(intent.Intent{Tool: "vm_reboot", Held: true, CutSend: oldTime})
	eng, err := toolEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	settling := open(intent.Intent{Tool: "redis_create", Name: "known", Key: eng.Maker(), Ended: true})
	closed := open(intent.Intent{Tool: "vm_reboot"})
	intent.Close(closed, history.Outcome{Result: history.Found})
	for i := 0; i < 16; i++ {
		open(intent.Intent{Tool: "vm_reboot", Name: fmt.Sprintf("vm-%d", i), Ended: true})
	}
	check := func(body string, count int) {
		t.Helper()
		var answer struct{ Checks []tools.DoctorCheck }
		if err := json.Unmarshal([]byte(body), &answer); err != nil {
			t.Fatal(err, body)
		}
		summaries, individual, settled := 0, 0, 0
		for _, c := range answer.Checks {
			switch c.Name {
			case "pending operation":
				if c.Status == tools.CheckOK {
					settled++
				} else {
					individual++
				}
			case "unresolved operations":
				summaries++
				if c.Status != tools.CheckWarn || c.Detail != fmt.Sprintf("The outcome of %d operations is unknown. Review their individual checks.", count) {
					t.Errorf("unexpected warning: %+v", c)
				}
			}
		}
		if (count == 19 && settled != 1) || (count > 19 && settled != 0) {
			t.Errorf("unknown=%d: %d settled records", count, settled)
		}
		want := 0
		if count >= 20 {
			want = 1
		}
		if summaries != want || individual != count+2 {
			t.Errorf("unknown=%d: got %d warnings and %d individual checks", count, summaries, individual)
		}
	}
	for count := 19; count <= 21; count++ {
		if count > 19 {
			open(intent.Intent{Tool: "vm_reboot", Ended: true})
		}
		code, out, stderr := runCLI(t, "", "doctor", "--json")
		if code != 0 {
			t.Fatal(code, stderr)
		}
		check(out, count)
		if _, err := os.Stat(filepath.Join(dir, settling.ID+".json")); !os.IsNotExist(err) {
			t.Fatalf("known outcome still pending: %v", err)
		}
		if count == 20 {
			for _, lang := range []string{i18n.RU, i18n.EN} {
				t.Setenv("EVO_LANG", lang)
				i18n.SetLang(lang)
				code, human, errOut := runCLI(t, "", "doctor")
				detail := "The outcome of 20 operations is unknown"
				if lang == i18n.RU {
					detail = "Число операций с неизвестным исходом — 20"
				}
				if code != 0 || !strings.Contains(human, detail) {
					t.Fatal(code, detail, human, errOut)
				}
			}
			t.Setenv("EVO_LANG", "ru")
			i18n.SetLang(i18n.RU)
			m := callStdioTool(t, "doctor", map[string]any{})
			if m.isErr {
				t.Fatal(m.text)
			}
			check(m.text, count)
		}
		if read(legacyPath) != legacy || read(foreignPath) != foreignBytes {
			t.Fatal("doctor modified a protected record")
		}
	}
}
