// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The id an operation names its resource by is the cloud's text: the read
// of every *_operation names that field in "_untrusted" — not only marks
// the answer for another field («Риск», conclusion 312, 312-2: ar, dns,
// k8s and lb marked error alone; kafka, pg, redis and vpc nothing).
func TestOperationRead_ResourceIDIsMarked(t *testing.T) {
	faultToolsIsolate(t)
	api := &oddAPI{status: http.StatusOK, body: hostileBody()}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)
	for _, tl := range r.Tools() {
		if !strings.HasSuffix(tl.Name, "_operation") {
			continue
		}
		m, text, _ := callNamed(t, r, tl.Name, requiredArgs(tl))
		u, _ := m["_untrusted"].([]any)
		for _, k := range []string{"resource_id", "resourceId"} {
			if v, _ := m[k].(string); v != "" && !slices.Contains(u, any(k)) {
				t.Errorf("%s: %s from the cloud, not in _untrusted %v: %.300s", tl.Name, k, u, text)
			}
		}
	}
}
