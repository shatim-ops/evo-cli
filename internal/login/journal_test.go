// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bufio"
	"encoding/json"
	"errors"
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

// The line a login ends with (Qwen on #2139, medium): a login whose undo
// left what it made — the account, the key — is unknown, not error: what
// is in the cloud is not known to be nothing; the lost answer of the key
// is unknown as before.
func TestLoginOutcome_AnUndoThatLeftIsUnknown(t *testing.T) {
	failed := errors.New("refused")
	for _, c := range []struct {
		err                  error
		keyUnknown, undoLeft bool
		want                 string
	}{
		{nil, false, false, history.Created},
		{failed, false, false, history.Error},
		{failed, false, true, history.Unknown},
		{failed, true, false, history.Unknown},
	} {
		if got := loginOutcome(c.err, c.keyUnknown, c.undoLeft).Result; got != c.want {
			t.Errorf("err %v, key unknown %v, undo left %v: %s, want %s", c.err, c.keyUnknown, c.undoLeft, got, c.want)
		}
	}
}
