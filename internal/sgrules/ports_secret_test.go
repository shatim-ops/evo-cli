// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"strings"
	"testing"
)

// A bad port spec is refused by its place in the list, not by its text
// (#2431; the owner's rule of #2459: the form, without the value).
func TestParsePorts_RefusalNamesThePlaceNotTheValue(t *testing.T) {
	for _, spec := range []string{"s3cr3t", "s3cr3t/tcp", "80/s3cr3t", "70000"} {
		_, _, err := ParsePorts([]string{"80", spec})
		if err == nil {
			t.Fatalf("%q: want a refusal", spec)
		}
		if strings.Contains(err.Error(), "s3cr3t") || strings.Contains(err.Error(), "70000") || !strings.Contains(err.Error(), "2") {
			t.Errorf("%q: want the place, not the value: %v", spec, err)
		}
	}
}
