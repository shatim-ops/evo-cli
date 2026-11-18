// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"strings"
	"testing"
)

// A refused chart or repo_url of k8s_helm over HTTP is not repeated: it may
// carry user:pass, and "user:pass@host" parses with "user" as its scheme
// (#2356).
func TestHelmHTTPCheck_DoesNotRepeatTheURL(t *testing.T) {
	const secret = "s3cr3tHelm27183650904512"
	for _, args := range []map[string]any{
		{"chart": "user:" + secret + "@charts.example/x"},
		{"chart": "./" + secret},
		{"chart": "http://user:" + secret + "@charts.example/x"},
		{"chart": "https://user:" + secret + "@127.0.0.1/x"},
		{"repo_url": "ftp://user:" + secret + "@charts.example"},
		{"repo_url": "user:" + secret + "@charts.example"},
		{"repo_url": "https://user:" + secret + "@10.0.0.1/charts"},
		{"repo_url": "https://user:" + secret + "@/x"},
		{"repo_url": "https://user:" + secret + "@[::1"},
	} {
		err := helmHTTPCheck(context.Background(), args)
		if err == nil {
			t.Fatalf("%v: want a refusal", args)
		}
		if strings.Contains(err.Error(), "s3cr3t") || strings.Contains(err.Error(), "user:") {
			t.Errorf("%v: the refusal repeats the URL: %v", args, err)
		}
	}
}
