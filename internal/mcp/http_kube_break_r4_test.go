// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"strings"
	"testing"
)

// describe печатает аннотации целиком, включая
// kubectl.kubernetes.io/last-applied-configuration — те же данные Secret,
// которые политика уже запрещает для annotate --list и
// apply view-last-applied. Без --allow-secret-export команда не должна
// проходить.
func TestBreakR4_R4_1_DescribeSecretNeedsExport(t *testing.T) {
	for _, c := range []string{
		"describe secret x",
		"describe secrets",
		"describe secret/x",
		"describe secrets/db",
		"describe cm,secret",
		"describe Secret.v1 x",
		"describe secret x -n db",
		"describe -n db secret x",
	} {
		if err := kubectlHTTPCheck(strings.Fields(c), false); err == nil {
			t.Errorf("%q is allowed over HTTP without --allow-secret-export", c)
		}
	}
}
