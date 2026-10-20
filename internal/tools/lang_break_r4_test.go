// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// В русском режиме CLI сообщение подтверждения provision — английская фраза.
// QA-11 эту ветку не смотрит: у ответа нет reason.
func TestBreakR4_R4_20_ProvisionConfirmSpeaksCLILanguage(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })

	reg := contractReg(t, &contractRT{kind: "500"})
	tl, ok := reg.Lookup("provision")
	if !ok {
		t.Fatal("no provision")
	}
	text, _, pan := contractCall(tl, map[string]any{"name": "web"}, time.Second)
	if pan != "" {
		t.Fatal(pan)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatal(err)
	}
	msg, _ := m["message"].(string)
	if phrase := englishPhrase(msg); phrase != "" {
		t.Fatalf("Russian CLI, English phrase %q in message %q", phrase, msg)
	}
}
