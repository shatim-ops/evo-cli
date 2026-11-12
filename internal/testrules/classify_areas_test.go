// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every file of tools has a live area (release plan 1.9, wave В5:
// «области cloud-e2e для нового файла — classify.sh»): classify.sh reads
// the paths of a pull request and names the areas of cloud-e2e to run
// before or after the merge — the checklist of the PR and the plan of a
// live run come from it. A file of tools it does not know changes the
// cloud with no area named: on 12.11 the files of security group rules
// and of subnets (tools_sg.go, tools_sg_create.go, tools_subnet.go) had
// none, with sg_rule_create merged the same day. The rule reads
// classify.sh as text — no shell runs, so it holds on every runner.

// toolFilesWithoutArea are the files internal/tools/tools*.go that
// classify.sh gives no area, with the reason. A closed list: a new file
// of tools takes an area or a line here.
var toolFilesWithoutArea = map[string]string{
	"internal/tools/tools.go":             "the registry of every group: its class is tools; the area is the one of the group's own file",
	"internal/tools/tools_admin.go":       "reads of the organization and its projects: nothing created to check live",
	"internal/tools/tools_audit.go":       "a read of the audit log",
	"internal/tools/tools_auth.go":        "whoami and agent_skill: reads with no cloud object",
	"internal/tools/tools_billing.go":     "reads of the billing",
	"internal/tools/tools_marketplace.go": "reads of the marketplace",
	"internal/tools/tools_monitoring.go":  "reads of the monitoring",
}

// toolFilesAreaPending are the files of tools whose live area does not
// exist yet: cloud-e2e has no area that calls their tools. Only shrinks.
// Closes by 1.9.
var toolFilesAreaPending = map[string]string{
	"internal/tools/tools_backup.go": "backups (wave В5): the area ops makes backups through the API, not through evo's tools; an area of its own comes with backup_delete",
}

// classifyAreaClause is a clause of classify.sh that names an area: the
// patterns before ") area <name> ;;".
var classifyAreaClause = regexp.MustCompile(`(?s)([^;]*?)\)\s*area\s+([a-z0-9]+)\s*;;`)

func TestClassify_EveryToolFileHasALiveArea(t *testing.T) {
	root := filepath.Join("..", "..")
	b, err := os.ReadFile(filepath.Join(root, ".github", "scripts", "classify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// The cases of the loop over the paths, with the line continuations
	// joined.
	text := strings.ReplaceAll(string(b), "\\\n", " ")
	var patterns []string
	for _, m := range classifyAreaClause.FindAllStringSubmatch(text, -1) {
		clause := m[1]
		if i := strings.LastIndex(clause, "case $p in"); i >= 0 {
			clause = clause[i+len("case $p in"):]
		}
		for _, p := range strings.Split(clause, "|") {
			if p = strings.TrimSpace(p); strings.HasPrefix(p, "internal/") {
				patterns = append(patterns, p)
			}
		}
	}
	if len(patterns) < 20 {
		t.Fatalf("classify.sh: %d patterns of areas read — the cases moved, update the test", len(patterns))
	}
	area := func(file string) bool {
		for _, p := range patterns {
			if ok, _ := path.Match(p, file); ok {
				return true
			}
			// A pattern of the shell's case crosses "/"; path.Match does
			// not — a directory pattern covers what is under it.
			if strings.HasSuffix(p, "/*") && strings.HasPrefix(file, strings.TrimSuffix(p, "*")) {
				return true
			}
		}
		return false
	}
	files, err := filepath.Glob(filepath.Join(root, "internal", "tools", "tools*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no files of tools: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		rel, _ := filepath.Rel(root, f)
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		_, declared := toolFilesWithoutArea[rel]
		_, pending := toolFilesAreaPending[rel]
		switch has := area(rel); {
		case !has && !declared && !pending:
			t.Errorf("%s: classify.sh names no area of cloud-e2e for it — a pull request that changes its tools gets no live check in its checklist; add the file to an area (.github/scripts/classify.sh), or to toolFilesWithoutArea with the reason", rel)
		case has && (declared || pending):
			t.Errorf("%s has an area in classify.sh now: remove its line from the list of the test", rel)
		}
	}
	var gone []string
	for _, list := range []map[string]string{toolFilesWithoutArea, toolFilesAreaPending} {
		for rel := range list {
			if !seen[rel] {
				gone = append(gone, rel)
			}
		}
	}
	sort.Strings(gone)
	for _, rel := range gone {
		t.Errorf("the test names %s, which is no file of tools: remove the line", rel)
	}
}
