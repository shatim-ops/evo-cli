// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// An argument of echoedArgs is a line a person reads in the preview: it has
// a label of paramLabels, not its key, and the tool has the argument.
func TestEchoedArgs_DeclarationsHold(t *testing.T) {
	r := New(nil)
	for tool, keys := range echoedArgs {
		tl, ok := r.Lookup(tool)
		if !ok {
			t.Errorf("echoedArgs names %s, which is no tool", tool)
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, k := range keys {
			if _, has := props[k]; !has {
				t.Errorf("echoedArgs[%s]: no argument %s", tool, k)
			}
			if engine.ParamLabel(k) == strings.ReplaceAll(k, "_", "-") {
				t.Errorf("echoedArgs[%s]: %s has no label in paramLabels", tool, k)
			}
		}
	}
}
