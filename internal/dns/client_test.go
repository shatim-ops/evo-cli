// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package dns

import (
	"encoding/json"
	"testing"
)

// resourceId (spec) and resource_id (as read so far) both give the id.
func TestOperation_ResourceID(t *testing.T) {
	for _, js := range []string{`{"id":"op","resourceId":"zone-1"}`, `{"id":"op","resource_id":"zone-1"}`} {
		var op Operation
		if err := json.Unmarshal([]byte(js), &op); err != nil || op.ResourceID != "zone-1" {
			t.Errorf("%s: %+v, %v", js, op, err)
		}
	}
}
