// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// The value of a secret argument does not come back in an answer, as it
// is, line by line, or in the forms JSON escaping gives it inside a
// service's text nested in a body; evo's own words and short values stay
// (Астра #2257).
func TestWithoutSecretArgs_MasksEveryFormOfTheValue(t *testing.T) {
	tpl := "#cloud-config\npassword: \"P@ss\\word42\""
	body, _ := json.Marshal(map[string]any{"user_data_template": tpl})
	nested, _ := json.Marshal(map[string]any{"message": "invalid request body: " + string(body)})
	answer := toJSON(map[string]any{
		"error": "create image: API returned status 400: " + string(nested),
		"hint":  "the template as sent: " + tpl,
		"next":  "image_list shows the images",
	})
	got := withoutSecretArgs("image_create", map[string]any{"user_data_template": tpl}, answer)
	for _, leak := range []string{"P@ss", "word42"} {
		if strings.Contains(got, leak) {
			t.Errorf("the secret %q is in the answer: %s", leak, got)
		}
	}
	if !strings.Contains(got, secretMask) || !strings.Contains(got, "image_list shows the images") {
		t.Errorf("the mask or evo's words are gone: %s", got)
	}
	// A tool without a secret argument, a call without the value: as it is.
	if withoutSecretArgs("image_list", map[string]any{"user_data_template": tpl}, answer) != answer ||
		withoutSecretArgs("image_create", map[string]any{"name": "x"}, answer) != answer {
		t.Error("an answer of a call without a secret argument changed")
	}
	// SecretFiles too: the password of pg_user_create.
	if got := withoutSecretArgs("pg_user_create", map[string]any{"password": "Sup3rSecret!"}, `{"error":"bad Sup3rSecret!"}`); strings.Contains(got, "Sup3rSecret") {
		t.Errorf("a password of SecretFiles is in the answer: %s", got)
	}
}
