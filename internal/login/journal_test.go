// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/history"
)

// journalOf reads the journal of changes beside the intents of dir: its
// lines and their raw text.
func journalOf(t *testing.T, dir string) (lines []history.Line, raw string) {
	t.Helper()
	hdir := history.DirOf(dir)
	entries, _ := os.ReadDir(hdir)
	var b strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(hdir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			var l history.Line
			if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
				t.Fatalf("not a line: %q", sc.Text())
			}
			lines = append(lines, l)
		}
	}
	return lines, b.String()
}
