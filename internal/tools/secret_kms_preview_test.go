// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// Full audit 1.5, FB150-5: secret_create with kms_key_id sends kmsKeyId —
// the key the secret is encrypted with — and its preview named only the
// path, so the person agreed without seeing it. The preview names the key
// and the description it sends; the value never.
func TestSecretCreate_PreviewNamesTheKMSKey(t *testing.T) {
	const kms = "kms-key-of-the-test"
	rt := &contractRT{kind: "200", body: `{"secrets":[],"items":[],"total":0}`}
	tl, _ := contractReg(t, rt).Lookup("secret_create")
	text, _, _ := contractCall(tl, map[string]any{"path": "app/db", "value": "never-in-a-preview",
		"kms_key_id": kms, "description": "db of the app"}, contractCallLimit)
	var m struct {
		Status  string `json:"status"`
		Preview struct {
			Params []struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			} `json:"params"`
		} `json:"preview"`
		WillCreate map[string]any `json:"will_create"`
	}
	if err := json.Unmarshal([]byte(text), &m); err != nil || m.Status != "confirmation_required" {
		t.Fatalf("secret_create preview: %v\n%s", err, text)
	}
	named := map[string]any{}
	for _, p := range m.Preview.Params {
		named[p.Key] = p.Value
	}
	if named["kms_key_id"] != kms || m.WillCreate["kms_key_id"] != kms {
		t.Errorf("the preview does not name kms_key_id %q: params %v, will_create %v", kms, named, m.WillCreate)
	}
	if named["description"] != "db of the app" {
		t.Errorf("the preview does not name the description: %v", named)
	}
	if strings.Contains(text, "never-in-a-preview") {
		t.Errorf("the value of the secret is in the preview:\n%s", text)
	}
}
