// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// k8s_helm refuses a repo_url or an action it cannot take without
// repeating it: a URL may carry user:pass, an action may be anything typed
// (#2356).
func TestK8sHelm_RefusalDoesNotRepeatTheValue(t *testing.T) {
	const secret = "s3cr3tHelmT27183650904512"
	r := newTestServer(t)
	for _, args := range []map[string]any{
		{"cluster_id": "00000000-0000-4000-8000-0000000000c1", "action": "install", "release": "web", "chart": "repo/web", "repo_url": "ftp://user:" + secret + "@charts.example"},
		{"cluster_id": "00000000-0000-4000-8000-0000000000c1", "action": "install", "release": "web", "chart": "repo/web", "repo_url": "user:" + secret + "@charts.example"},
		{"cluster_id": "00000000-0000-4000-8000-0000000000c1", "action": secret},
	} {
		m, text, isErr := callNamed(t, r, "k8s_helm", args)
		if !isErr || m["reason"] != "USAGE" {
			t.Fatalf("%v: want USAGE: %s", args, text)
		}
		if strings.Contains(text, "s3cr3t") {
			t.Errorf("%v: the refusal repeats the value: %s", args, text)
		}
	}
}
