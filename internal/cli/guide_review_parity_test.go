// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"strings"
	"testing"
)

// TestReviewParity_RP_7_GuideSecurityGroupsViaToolsCall: list_security_groups
// has the command evo vpc security-groups. docs/guide.md still tells the
// reader to list groups with evo tools call.
func TestReviewParity_RP_7_GuideSecurityGroupsViaToolsCall(t *testing.T) {
	b, err := os.ReadFile(repoRoot + "/docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "evo tools call list_security_groups") {
		t.Fatal("docs/guide.md points at `evo tools call list_security_groups`; the command of that tool is `evo vpc security-groups`")
	}
}
