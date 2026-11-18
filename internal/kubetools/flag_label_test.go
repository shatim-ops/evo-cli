// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package kubetools

import (
	"strings"
	"testing"
)

// A refused kubectl flag is named without its value: --token=… and
// --password=… carry a secret (#2353).
func TestCheckConnectionFlags_NameTheFlagNotItsValue(t *testing.T) {
	const secret = "s3cr3tK8s27183650904512"
	for _, f := range []string{"--token=" + secret, "--password=" + secret, "--server=https://" + secret, "--kuberc=" + secret, "-s" + secret} {
		err := CheckConnectionFlags([]string{"get", "pods", f})
		if err == nil {
			t.Fatalf("%s: want a refusal", f)
		}
		if strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%s: the refusal repeats the value: %v", f, err)
		}
	}
}
