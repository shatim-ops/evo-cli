// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"fmt"
	"testing"
)

// A create the API answered with success but no id is the API's fault:
// API_ERROR, exit 5, not USAGE (the caller's arguments were fine).
func TestNoID_IsAPIError(t *testing.T) {
	var m map[string]any
	text := errResult(fmt.Errorf("create cluster: %w", noID("operation")))
	if err := json.Unmarshal([]byte(text), &m); err != nil || m["reason"] != "API_ERROR" {
		t.Errorf("reason %v, want API_ERROR: %s", m["reason"], text)
	}
}
