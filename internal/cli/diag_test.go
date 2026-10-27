// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// evo diag on turns the journal on in config.yaml; a command of the CLI is
// then a record of the "cli" surface, status counts it, and export copies
// the file without values (D46). Off, nothing is written.
func TestDiag_OnRecordStatusExport(t *testing.T) {
	setupCLI(t)
	const secretish = "value-that-must-not-leak-0123"
	runCLI(t, "", "tools", "call", "secret_create", "--arg", "path=app/db", "--arg", "value="+secretish)
	status := func() map[string]any {
		t.Helper()
		code, out, errOut := runCLI(t, "", "--json", "diag", "status")
		if code != 0 {
			t.Fatalf("diag status: %d %s", code, errOut)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return m
	}
	if m := status(); m["enabled"] != false || m["records"] != float64(0) {
		t.Fatalf("the journal is off by default and empty: %v", m)
	}
	if code, _, errOut := runCLI(t, "", "diag", "on"); code != 0 {
		t.Fatalf("diag on: %s", errOut)
	}
	// Without --yes the call stops at the preview: the tool was called.
	if code, _, errOut := runCLI(t, "", "tools", "call", "secret_create", "--arg", "path=app/db", "--arg", "value="+secretish); code != 3 {
		t.Fatalf("secret_create without --yes: exit %d, want 3: %s", code, errOut)
	}
	m := status()
	if m["enabled"] != true || m["records"] != float64(1) || m["sessions"] != float64(1) {
		t.Fatalf("one call after diag on: %v", m)
	}
	data, err := os.ReadFile(m["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secretish) || !strings.Contains(string(data), `"surface":"cli"`) ||
		!strings.Contains(string(data), `"tool":"secret_create"`) || !strings.Contains(string(data), `"preview":true`) {
		t.Fatalf("journal:\n%s", data)
	}
	out := filepath.Join(t.TempDir(), "j.jsonl")
	if code, _, errOut := runCLI(t, "", "diag", "export", "--out", out); code != 0 {
		t.Fatalf("diag export: %s", errOut)
	}
	if got, _ := os.ReadFile(out); string(got) != string(data) {
		t.Error("the export is not the journal")
	}
	if code, _, _ := runCLI(t, "", "diag", "export", "--out", out); code != 1 {
		t.Errorf("export over an existing file: exit %d, want 1", code)
	}
	if code, _, _ := runCLI(t, "", "diag", "off"); code != 0 || status()["enabled"] != false {
		t.Error("diag off")
	}
}
