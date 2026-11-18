// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The id disk_create answers is the cloud's — the create gave it, the call
// did not — and so is the same id in the arguments of the read it names
// next: both are in "_untrusted" (Астра, #2499).
func TestDiskCreate_TheCloudsIDIsMarked(t *testing.T) {
	faultToolsIsolate(t)
	api := &oddAPI{status: http.StatusOK, body: hostileBodyIn("creating", "creating",
		map[string]any{"id": "disk-CLOUD-MARK", "vms": []any{}})}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	cloud.Backoff = 1
	r := raRegistry(t, api, cloud, nil)
	r.SetCallBudget(containerBudget)
	_, text, _ := callNamed(t, r, "disk_create", map[string]any{
		"name": "data", "size_gb": 10, "zone": "00000000-0000-4000-8000-0000000000f1", "confirmed": true,
	})
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil || m["disk_id"] != "disk-CLOUD-MARK" || m["next_args"] == nil {
		t.Fatalf("the world did not reach the fields: %s", text)
	}
	marked, _ := m["_untrusted"].([]any)
	for _, f := range []string{"disk_id", "next_args"} {
		if !slices.Contains(marked, any(f)) {
			t.Errorf("%s carries the cloud's id outside _untrusted %v", f, marked)
		}
	}
}
