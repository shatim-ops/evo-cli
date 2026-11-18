// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"strings"
	"testing"
)

// The HTTP policy names a refused kubectl flag without its value: the
// value may be a token or a password (#2353).
func TestKubectlCheck_NamesTheFlagNotItsValue(t *testing.T) {
	const secret = "s3cr3tHTTP27183650904512"
	for _, w := range []wording{httpWording, readOnlyWording} {
		for _, f := range []string{"--" + secret + "file=x", "--" + secret + "-dir=x", "--" + secret + "_file", "--" + secret + "_file=x", "--" + secret + "file", "--token=" + secret, "--password=" + secret, "--client-key=" + secret, "--edit=" + secret, "--v=" + secret, "-v" + secret} {
			err := kubectlCheck([]string{"get", "pods", f}, false, w)
			if err == nil {
				t.Fatalf("%s: want a refusal", f)
			}
			if strings.Contains(strings.ToLower(err.Error()), "s3cr3t") {
				t.Errorf("%s: the refusal repeats the value: %v", f, err)
			}
		}
		// A flag of the closed list is still named, its value is not.
		err := kubectlCheck([]string{"get", "pods", "--kubeconfig=" + secret}, false, w)
		if err == nil || !strings.Contains(err.Error(), "--kubeconfig") || strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("--kubeconfig: want the flag named without its value: %v", err)
		}
	}
}
