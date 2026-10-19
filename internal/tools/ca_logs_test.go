// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/containerapp"
)

// Live run 18.10: ca_logs answered {"logs":[]} and nothing else. An empty
// answer now says what the logs are and what to try next.
func TestCaLogsOutput_EmptyHasNote(t *testing.T) {
	out := caLogsOutput(&containerapp.LogsResult{Lines: []map[string]any{}}, false)
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"logs":[]`) {
		t.Fatalf("logs must stay an empty list: %s", b)
	}
	note, _ := out["note"].(string)
	for _, want := range []string{"stdout", caLogsDoc, "system=true"} {
		if !strings.Contains(note, want) {
			t.Fatalf("note %q lacks %q", note, want)
		}
	}
	sys, _ := caLogsOutput(&containerapp.LogsResult{Lines: []map[string]any{}}, true)["note"].(string)
	if !strings.Contains(sys, "No system events") {
		t.Fatalf("system note = %q", sys)
	}
}

func TestCaLogsOutput_Lines(t *testing.T) {
	lines := []map[string]any{{"message": "a"}}
	out := caLogsOutput(&containerapp.LogsResult{Lines: lines, Read: 1}, false)
	if _, ok := out["note"]; ok {
		t.Fatalf("no note expected with lines: %v", out)
	}
	cut := caLogsOutput(&containerapp.LogsResult{Lines: lines, Read: 5000, Cut: true}, false)
	if cut["truncated"] != true || !strings.Contains(cut["note"].(string), "5000") {
		t.Fatalf("cut answer = %v", cut)
	}
}
