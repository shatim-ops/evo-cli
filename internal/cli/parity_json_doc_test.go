// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"strings"
	"testing"
)

// docs/mcp.md «Ответ инструмента и --json команды» lists every exception of
// the JSON parity by a row of its table and names their number in words:
// an exception the code keeps (jsonAliasCompat) and the guide does not
// name is a contract the integrator does not see (Astra 2067, #2078: the
// guide said six while the code kept seven, evo agent skill unnamed).
func TestParity_JSONExceptionsAreInTheGuide(t *testing.T) {
	b, err := os.ReadFile("../../docs/mcp.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	for path := range jsonAliasCompat {
		if !strings.Contains(doc, "| `"+path+"` |") && !strings.Contains(doc, "| `"+path+" ") {
			t.Errorf("docs/mcp.md has no row of the JSON parity exception %q", path)
		}
	}
	words := map[int]string{5: "пять", 6: "шесть", 7: "семь", 8: "восемь", 9: "девять", 10: "десять"}
	want, ok := words[len(jsonAliasCompat)]
	if !ok {
		t.Fatalf("%d exceptions: add the word for it", len(jsonAliasCompat))
	}
	if !strings.Contains(doc, "Исключений "+want+",") {
		t.Errorf("docs/mcp.md does not say «Исключений %s,»: the code keeps %d", want, len(jsonAliasCompat))
	}
}
