// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// Why a delete's state is unseen is said in words: a document of the
// development of evo is named in a comment of the declaration, not in its
// text (#2348).
func TestDeleteTarget_WhyUnseenNamesNoDocumentOfDevelopment(t *testing.T) {
	for name, d := range decls {
		for _, why := range []string{d.Delete.creatingUnseen, d.Delete.failedUnseen} {
			if strings.Contains(why, "docs/") {
				t.Errorf("%s: %q", name, why)
			}
		}
	}
}
