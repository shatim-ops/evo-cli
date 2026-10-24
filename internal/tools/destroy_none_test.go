// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// W5 (owner's decision 24.10, option b): destroy in a directory with no
// deployment asked for consent on an empty preview, and with consent
// answered status ok with a log. It answers what 'evo destroy --json' has
// given since v1.0 — status none with a message, no error — with no
// question, whatever confirmed says.
func TestDestroy_NoDeploymentIsNoneWithoutQuestion(t *testing.T) {
	r, _ := fastRegistry(t, &targetCloud{}, config.Config{ProjectID: "p"})
	for _, confirmed := range []bool{false, true} {
		m, text, isErr := callNamed(t, r, "destroy", map[string]any{"confirmed": confirmed})
		if isErr || m["status"] != "none" || m["message"] == nil {
			t.Errorf("destroy without a deployment, confirmed=%v: %s", confirmed, text)
		}
	}
}
