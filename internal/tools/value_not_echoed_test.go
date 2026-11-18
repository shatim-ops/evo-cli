// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// A value the call was given that evo cannot take is not repeated in the
// refusal or the preview: it may be anything typed, a secret included
// (#2353, as #2333, #2341, #2352).
func TestRefusalsAndPreviews_DoNotRepeatTheValue(t *testing.T) {
	const secret = "s3cr3tVal27183650904512"
	r := newTestServer(t)
	for name, args := range map[string]map[string]any{
		"history since": {"since": secret},
		"kubectl verb":  {"cluster_id": "00000000-0000-4000-8000-0000000000c1", "args": "--" + secret},
		"kubectl word":  {"cluster_id": "00000000-0000-4000-8000-0000000000c1", "args": secret + " pods"},
		"kubectl flag":  {"cluster_id": "00000000-0000-4000-8000-0000000000c1", "args": "get pods --token=" + secret},
	} {
		tool := "k8s_kubectl"
		if strings.HasPrefix(name, "history") {
			tool = "history"
		}
		_, text, _ := callNamed(t, r, tool, args)
		if strings.Contains(text, "s3cr3t") {
			t.Errorf("%s: the answer repeats the value: %s", name, text)
		}
	}
	// A verb of kubectl's own is still named.
	if _, text, _ := callNamed(t, r, "k8s_kubectl", map[string]any{"cluster_id": "00000000-0000-4000-8000-0000000000c1", "args": "delete pod web"}); !strings.Contains(text, `kubectl \"delete\"`) {
		t.Errorf("the preview of delete does not name it: %s", text)
	}
}
