// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"strings"
	"testing"
)

// REL-5: «apply edit-last-applied» и «create --edit» запускают редактор на
// хосте MCP-сервера, как edit. По HTTP — отказ, и с --allow-secret-export
// тоже: это хост, а не данные секрета.
func TestKubectlHTTP_EditorSubcommandsRefused(t *testing.T) {
	for _, allow := range []bool{false, true} {
		for _, c := range []string{
			"apply edit-last-applied deploy/web",
			"apply -n default edit-last-applied deploy/web",
			"apply edit-last-applied deploy/web -o json",
			"create deployment web --image=nginx --edit",
			"create deployment web --image=nginx --edit=true",
			"edit deploy/web",
		} {
			if err := kubectlCheck(strings.Fields(c), allow, httpWording); err == nil {
				t.Errorf("%q (allow-secret-export=%v) is allowed over HTTP", c, allow)
			}
		}
	}
	// Остальные подкоманды apply — как раньше.
	for _, c := range []string{
		"apply set-last-applied deploy/web -o yaml",
		"apply view-last-applied deploy/web",
		"create deployment web --image=nginx",
	} {
		if err := kubectlCheck(strings.Fields(c), false, httpWording); err != nil {
			t.Errorf("%q: %v", c, err)
		}
	}
}
