// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"strings"
	"testing"
)

// A flag refused by its form — "file" in its name, a name ending in -dir —
// may be any word the caller typed, a secret included: the refusal names
// its place, not its name; a flag of the closed list is still named
// (Астра #2369).
func TestKubectlCheck_NamesOnlyAKnownFlag(t *testing.T) {
	const secret = "s3cr3tastra27183650904512"
	for _, w := range []wording{httpWording, readOnlyWording} {
		for _, f := range []string{"--" + secret + "file=x", "--" + secret + "-dir=x", "--" + secret + "_file=x", "--" + secret + "file"} {
			err := kubectlCheck([]string{"get", "pods", f}, false, w)
			if err == nil {
				t.Fatalf("%s: want a refusal", f)
			}
			if strings.Contains(strings.ToLower(err.Error()), "s3cr3t") {
				t.Errorf("%s: the refusal repeats the name: %v", f, err)
			}
			if !strings.Contains(err.Error(), "argument 3") {
				t.Errorf("%s: the refusal does not name the place: %v", f, err)
			}
		}
		if err := kubectlCheck([]string{"get", "pods", "--kubeconfig=x"}, false, w); err == nil || !strings.Contains(err.Error(), "--kubeconfig") {
			t.Errorf("a known flag is not named: %v", err)
		}
	}
}
