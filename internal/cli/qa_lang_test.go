// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
	"unicode"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// QA-11 (QA 20.10): при русском языке CLI message ошибки инструмента был
// английским при русском hint: `evo vm create web --image U --image-id
// <uuid>` — «provision: pass image or image_id, not both» и «Проверьте
// аргументы». Сообщения и подсказки — на языке CLI (docs/exit-codes.md); с
// EVO_LANG=en — без кириллицы. Обход всех инструментов —
// TestQA11_ToolErrorsSpeakCLILanguage в internal/tools.

const qaLangUUID = "00000000-0000-4000-8000-000000000001"

func hasCyrillicText(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

func TestQA11_ToolErrorsInCLILanguage(t *testing.T) {
	cases := []struct {
		args   []string
		reason string
		ru, en string // a piece of the message in each language
	}{
		{[]string{"vm", "create", "web", "--image", "Ubuntu-24.04", "--image-id", qaLangUUID},
			clerrors.CodeUsage, "не оба", "not both"},
		{[]string{"k8s", "create", "--name", "web", "--master-count", "2"},
			clerrors.CodeUsage, "master_count должен быть 1, 3 или 5", "master_count must be 1, 3 or 5"},
		{[]string{"tools", "call", "dns_zone_create", "--arg", "domain=example.com.", "--arg", "type=bogus"},
			clerrors.CodeUsage, "не входит в список", "is not one of"},
		{[]string{"s3", "buckets"},
			clerrors.CodeConfigInvalid, "не заданы ключи S3", "S3 credentials not configured"},
		// QA-11, the same round: both arguments of a pair were one taken
		// silently.
		{[]string{"tools", "call", "vm_get", "--arg", "vm_id=" + qaLangUUID, "--arg", "name=web"},
			clerrors.CodeUsage, "укажите vm_id или name, не оба", "pass vm_id or name, not both"},
		{[]string{"tools", "call", "image_get", "--arg", "image_id=" + qaLangUUID, "--arg", "image=Ubuntu-24.04"},
			clerrors.CodeUsage, "укажите image или image_id, не оба", "pass image or image_id, not both"},
	}
	for _, lang := range []string{i18n.RU, i18n.EN} {
		t.Run(lang, func(t *testing.T) {
			old := i18n.Lang()
			i18n.SetLang(lang)
			t.Cleanup(func() { i18n.SetLang(old) })
			fc := setupCLI(t)
			for _, c := range cases {
				code, _, stderr := runCLI(t, "", append([]string{"--json"}, c.args...)...)
				e := lastJSONError(t, stderr)
				want := c.ru
				if lang == i18n.EN {
					want = c.en
				}
				if e.Reason != c.reason || code != clerrors.ExitCode(clerrors.New(c.reason, "", "")) || !strings.Contains(e.Message, want) {
					t.Errorf("%v: exit %d, %+v; want %s and %q in the message", c.args, code, e, c.reason, want)
				}
				if lang == i18n.EN && (hasCyrillicText(e.Message) || hasCyrillicText(e.Hint)) {
					t.Errorf("%v: Cyrillic in English: %+v", c.args, e)
				}
				if lang == i18n.RU && (!hasCyrillicText(e.Message) || (e.Hint != "" && !hasCyrillicText(e.Hint))) {
					t.Errorf("%v: not Russian: %+v", c.args, e)
				}
			}
			if n := fc.iamCalls.Load(); n != 0 {
				t.Errorf("IAM asked %d times", n)
			}
		})
	}
}
