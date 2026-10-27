// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// stateRoles are the roles of CLAUDE.md §0 ("Роли сессий"), each with its
// state file docs/internal/state/<file>.md: what a new session of the role
// reads first, a slice of now, not its history (owner's decision 27.10,
// 1.4.0).
var stateRoles = []string{"release", "development", "process", "product"}

// stateMissing are the roles whose file is not written yet, each with the
// PR that writes it. The list only shrinks: a line whose file is there
// fails the test.
var stateMissing = map[string]string{}

// stateSections are the headings of a state file, in this order, after the
// first line "Версия: X.Y.Z".
var stateSections = []string{"Очередь", "Ждёт решения", "Постоянные разрешения", "Стопы и лимит", "Известное красное"}

const (
	stateMaxWords = 300
	stateMaxQueue = 8
)

var (
	stateVersion = regexp.MustCompile(`^Версия: \d+\.\d+\.\d+$`)
	stateItem    = regexp.MustCompile(`^(\d+\.|-) `)
	stateUUID    = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	stateIPv4    = regexp.MustCompile(`\b(\d{1,3}\.){3}\d{1,3}\b`)
)

// A state file is short enough to read first and shaped the same for every
// role: the version on its first line, the five sections in order, at most
// stateMaxWords words and stateMaxQueue items in its queue, and nothing of
// the cloud — no UUID, no IP (the repository is public, CLAUDE.md §4).
func TestRoleStateFiles(t *testing.T) {
	dir := filepath.Join("..", "..", "docs", "internal", "state")
	for _, role := range stateRoles {
		path := filepath.Join(dir, role+".md")
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			if stateMissing[role] == "" {
				t.Errorf("%s: no state file for the role %s", filepath.ToSlash(path), role)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if stateMissing[role] != "" {
			t.Errorf("stateMissing names %s, whose file is there: remove the line", role)
		}
		checkStateFile(t, filepath.ToSlash(path), string(b))
	}
	known := map[string]bool{}
	for _, r := range stateRoles {
		known[r+".md"] = true
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	for _, f := range files {
		if !known[filepath.Base(f)] {
			t.Errorf("%s: a state file of no role in stateRoles", filepath.ToSlash(f))
		}
	}
}

func checkStateFile(t *testing.T, path, text string) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if !stateVersion.MatchString(lines[0]) {
		t.Errorf("%s: the first line is %q, want \"Версия: X.Y.Z\"", path, lines[0])
	}
	if n := len(strings.Fields(text)); n > stateMaxWords {
		t.Errorf("%s: %d words, at most %d — the history goes to the handoff file, not here", path, n, stateMaxWords)
	}
	var got []string
	queue, inQueue := 0, false
	for _, l := range lines {
		if h, ok := strings.CutPrefix(l, "## "); ok {
			got = append(got, h)
			inQueue = h == stateSections[0]
			continue
		}
		if inQueue && stateItem.MatchString(l) {
			queue++
		}
	}
	if strings.Join(got, "|") != strings.Join(stateSections, "|") {
		t.Errorf("%s: sections %q, want %q in this order", path, got, stateSections)
	}
	if queue > stateMaxQueue {
		t.Errorf("%s: %d items in the queue, at most %d", path, queue, stateMaxQueue)
	}
	if m := stateUUID.FindString(text); m != "" {
		t.Errorf("%s: a UUID (%s…) — the repository is public", path, m[:8])
	}
	if m := stateIPv4.FindString(text); m != "" {
		t.Errorf("%s: an IP address — the repository is public", path)
	}
}
