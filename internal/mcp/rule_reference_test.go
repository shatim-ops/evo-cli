// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestHTTPPolicy_ClosedList checks that the rows in docs/mcp.md match
// HTTPRestrictions. The contributor guide must point to that same range.
func TestMCPRuleReference(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	ref := regexp.MustCompile("docs/mcp\\.md`?,[[:blank:]]*(H[0-9]+)[[:blank:]]*[-–—][[:blank:]]*(H[0-9]+)\\b")
	matches := ref.FindAllStringSubmatch(string(guide), -1)
	if len(matches) == 0 || len(HTTPRestrictions) == 0 {
		t.Fatal("missing MCP restriction range in CLAUDE.md or empty HTTPRestrictions")
	}
	first, last := HTTPRestrictions[0].ID, HTTPRestrictions[len(HTTPRestrictions)-1].ID
	for _, match := range matches {
		if match[1] != first || match[2] != last {
			t.Errorf("CLAUDE.md links to %s–%s in docs/mcp.md, registry covers %s–%s", match[1], match[2], first, last)
		}
	}
}
