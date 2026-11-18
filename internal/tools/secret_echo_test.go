// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/secretsent"
)

// A call that sent the cloud a value that may be a secret (secretsent: a
// cloud-init template, the env of an app, the metadata of a VM) is refused
// by class, as a secret write is: the service's text, which may echo the
// value in any form, is not shown; the fields of the answer that are evo's
// stay as they are. A call that sent none keeps the service's text (Астра
// #2257; «Риск», conclusion 328; the audit of 1.9 rc.1, A19-2, #2419).
func TestSecretWriteAnswer_ACallThatSentASecret(t *testing.T) {
	refusal := toJSON(map[string]any{"status": "error", "reason": "API_ERROR", apiStatusField: 400,
		"error": "create image: invalid body: password: P@ss12345", "hint": "evo's hint", "_untrusted": []string{"error"},
		"details": map[string]any{"cause": "quota", "platform_code": "P@ss12345_quota_exceeded"}})
	sent := secretsent.Watch(context.Background())
	secretsent.Mark(sent)
	got := secretWriteAnswer(sent, "image_create", map[string]any{"user_data_template": "#cloud-config\npassword: P@ss12345"}, refusal, true)
	var m map[string]any
	_ = json.Unmarshal([]byte(got), &m)
	if d, _ := m["details"].(map[string]any); strings.Contains(got, "P@ss") || m["reason"] != "API_ERROR" || m["status"] != "error" || m["hint"] != "evo's hint" || d["cause"] != "quota" {
		t.Errorf("a refusal of a call that sent a secret: %s; want the class, the cause evo names, no text of the service", got)
	}
	if plain := secretWriteAnswer(secretsent.Watch(context.Background()), "image_create", map[string]any{"name": "img"}, refusal, true); plain != refusal {
		t.Errorf("a call that sent no secret lost the service's text: %s", plain)
	}
	ok := toJSON(map[string]any{"status": "building", "image": map[string]any{"id": "i-1"}})
	if got := secretWriteAnswer(sent, "image_create", map[string]any{"user_data_template": "x"}, ok, false); got != ok {
		t.Errorf("a success changed: %s", got)
	}
}
