// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// An existing bucket's name S3 cannot take ("../x", "a/b") is USAGE in the
// language of the CLI, before the preview and before the client: minio-go
// 7.4 refuses a path-shaped name with its own English text, which reached
// the person (CI of Dependabot #166), and s3_put/s3_rm asked for consent
// on a call that could not pass. Every tool that takes "bucket" is walked:
// a new one without the check fails here.
func TestS3_BucketNameRefusedInCLILanguage(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	// An engine with no S3 keys: a call that passes the check stops at
	// the client's CONFIG_INVALID, before the network.
	r := New(func(map[string]any) (*engine.Engine, error) { return &engine.Engine{}, nil })
	tools := 0
	for _, tl := range r.Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		if _, ok := props["bucket"]; !ok {
			continue
		}
		tools++
		for _, bad := range []string{"../x", "a/b", `a\b`} {
			args := map[string]any{"bucket": bad}
			if _, ok := props["key"]; ok {
				args["key"] = "k"
			}
			m, text, isErr := callNamed(t, r, tl.Name, args)
			msg, _ := m["error"].(string)
			if !isErr || m["reason"] != "USAGE" || englishPhrase(msg) != "" || !strings.Contains(msg, "бакета") {
				t.Errorf("%s %q: %s", tl.Name, bad, text)
			}
		}
	}
	if tools < 6 {
		t.Fatalf("%d tools take bucket", tools)
	}
	// A name minio takes goes on to the client (here: no S3 keys).
	m, text, _ := callNamed(t, r, "s3_ls", map[string]any{"bucket": "my-bucket"})
	if m["reason"] != "CONFIG_INVALID" {
		t.Errorf("a valid name refused: %s", text)
	}
}
