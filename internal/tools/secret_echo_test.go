// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// A call of image_create that carries a template is refused by class, as a
// secret write is: the service's text, which may echo the template in any
// form, is not shown; the fields of the answer that are evo's stay as they
// are. A call without a template keeps the service's text (Астра #2257;
// «Риск», conclusion 328).
func TestSecretWriteAnswer_ImageCreateWithATemplate(t *testing.T) {
	refusal := toJSON(map[string]any{"status": "error", "reason": "API_ERROR", apiStatusField: 400,
		"error": "create image: invalid body: password: P@ss12345", "hint": "evo's hint", "_untrusted": []string{"error"}})
	got := secretWriteAnswer(context.Background(), "image_create", map[string]any{"user_data_template": "#cloud-config\npassword: P@ss12345"}, refusal, true)
	var m map[string]any
	_ = json.Unmarshal([]byte(got), &m)
	if strings.Contains(got, "P@ss") || m["reason"] != "API_ERROR" || m["status"] != "error" || m["hint"] != "evo's hint" {
		t.Errorf("a refusal of a call with a template: %s", got)
	}
	if plain := secretWriteAnswer(context.Background(), "image_create", map[string]any{"name": "img"}, refusal, true); plain != refusal {
		t.Errorf("a call without a template lost the service's text: %s", plain)
	}
	ok := toJSON(map[string]any{"status": "building", "image": map[string]any{"id": "i-1"}})
	if got := secretWriteAnswer(context.Background(), "image_create", map[string]any{"user_data_template": "x"}, ok, false); got != ok {
		t.Errorf("a success changed: %s", got)
	}
}
