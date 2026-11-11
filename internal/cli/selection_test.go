// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// A list answered under filters is shown as a list, with the selection
// named over the table: the filters, how many passed and of how many
// (briefs/list-filters.md, «Согласованность», 4). An empty selection says
// so, with the tool's hint, instead of «No items».
func TestRenderAnswer_NamesTheSelection(t *testing.T) {
	show := func(text string) string {
		var buf bytes.Buffer
		p := output.NewWithWriter(&buf, output.ModeText, false)
		if err := renderAnswer(p, &buf, text, tableView{Columns: []string{"id", "name", "state"}}); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	out := show(`{"vms":[{"id":"1","name":"web-1","state":"running"}],"filters":{"zone":"ru.AZ-2 (a2)","state":"running"},"count":1,"total":12,"filtered_by":"evo"}`)
	for _, want := range []string{"state running", "zone ru.AZ-2 (a2)", "1 of 12", "web-1", "NAME"} {
		if !strings.Contains(out, want) {
			t.Errorf("the selection is shown without %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "filtered_by") || strings.Contains(out, "FIELD") {
		t.Errorf("the answer under filters is not shown as a list:\n%s", out)
	}
	if strings.Index(out, "1 of 12") > strings.Index(out, "web-1") {
		t.Errorf("the selection is named over the table:\n%s", out)
	}

	empty := show(`{"vms":[],"filters":{"state":"stopped"},"count":0,"total":12,"filtered_by":"evo","hint":"Nothing passed the filters; 12 without them."}`)
	if !strings.Contains(empty, "Nothing passed the filters: state stopped — 0 of 12.") || !strings.Contains(empty, "12 without them") {
		t.Errorf("an empty selection names the filters and carries the hint:\n%s", empty)
	}
	if strings.Contains(empty, "No items") {
		t.Errorf("an empty selection is not «No items»:\n%s", empty)
	}

	// Without filters the list is shown as before.
	if plain := show(`{"vms":[{"id":"1","name":"web-1","state":"running"}]}`); strings.Contains(plain, "Selection") {
		t.Errorf("a list without filters names a selection:\n%s", plain)
	}
}
