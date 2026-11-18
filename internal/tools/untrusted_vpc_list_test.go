// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A list of VPCs that is not empty names them as the cloud gave them: the
// answer marks "vpcs", with a filter of the name and without (Астра, the
// agent run of rc.1, #2524: 3 of 3 answers without _untrusted; the world of
// the class had no "vpcs" in its body).
func TestUntrusted_VPCListMarked(t *testing.T) {
	faultToolsIsolate(t)
	vpc := map[string]any{"id": "00000000-0000-4000-8000-0000000000f1", "name": "web\n" + cloudMark}
	var base any
	if err := json.Unmarshal(hostileBody(map[string]any{"vpcs": []any{vpc}}), &base); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(base)
	api := &countingRT{RoundTripper: &oddAPI{status: http.StatusOK, body: body}}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)
	for _, args := range []map[string]any{{}, {"name": "web\n" + cloudMark}} {
		m, text, isErr := callNamed(t, r, "vpc_list", args)
		if isErr {
			t.Fatalf("%v: %s", args, text)
		}
		// filters echo the caller's own argument, not a value of the cloud.
		delete(m, "filters")
		if leaks := classLeaks(m); len(leaks) > 0 {
			t.Errorf("%v: the names of the VPCs outside \"_untrusted\" %v: %v", args, m["_untrusted"], leaks)
		}
	}
}
