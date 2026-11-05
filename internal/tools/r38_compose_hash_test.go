// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"
)

// «Риск · 1.8», R-38 (D82): the step that goes on with a deploy carries
// confirmed — one consent — and the repeat opens the ports of the compose
// file as it is then: a port added after consent opened without a
// question. The step carries the hash of the compose file the consent was
// for (compose_hash); a confirmed deploy whose file has another hash now
// answers the preview again, and writes nothing.
func TestDeploy_ComposeChangedSinceConsentAsksAgain(t *testing.T) {
	cloud := &productCloud{}
	r, _ := cancelRegistry(t, cloud)
	m, text, _ := callNamed(t, r, "deploy", map[string]any{"confirmed": true, "compose_hash": "0000000000000000000000000000000000000000000000000000000000000000"})
	if m["status"] != "confirmation_required" {
		t.Errorf("a confirmed deploy of a compose file changed since consent went on: %s", text)
	}
	if len(cloud.mutations) != 0 {
		t.Errorf("written before the new question: %v", cloud.mutations)
	}
}
