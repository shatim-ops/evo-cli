// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"regexp"
	"testing"
)

// registerCode is the form of an item of the closed register of
// discrepancies: the prefix of its section, a dash, a number.
var registerCode = regexp.MustCompile(`\b(AR|AUD|BIL|CAP|CMP|CRT|DB|DOC|DP|FM|IAM|IMG|K8S|NET|NLB|OPS|SEC)-\d+\b`)

// What an agent reads through an installed evo — the description and the
// schema of each tool, in both languages, and the card of each group —
// names no item of the closed register: the register is not public, and a
// code tells the agent nothing it can look up. The limit itself is said in
// words (owner's decision 14.11, public trace, item 3).
func TestAgentText_NoRegisterCodes(t *testing.T) {
	r := New(nil)
	groups := map[string]bool{}
	for _, tl := range r.Tools() {
		schema, _ := json.Marshal(tl.InputSchema)
		ru, _ := json.Marshal(tl.ArgDescriptionsRU)
		for _, text := range []string{tl.Description, string(schema), tl.ShortRU, tl.DescriptionRU, string(ru)} {
			if c := registerCode.FindString(text); c != "" {
				t.Errorf("%s: names %s of the register", tl.Name, c)
			}
		}
		groups[tl.Group] = true
	}
	for g := range groups {
		if card, ok := GroupCard(g); ok {
			if c := registerCode.FindString(card); c != "" {
				t.Errorf("card %s: names %s of the register", g, c)
			}
		}
	}
}
