// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"regexp"
	"strconv"
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
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "Исключений "+want) {
		t.Errorf("README.md must give the registered exception count (%d)", len(jsonAliasCompat))
	}
	if !strings.Contains(doc, "Исключений "+want+",") {
		t.Errorf("docs/mcp.md does not say «Исключений %s,»: the code keeps %d", want, len(jsonAliasCompat))
	}
}

// The prose introduces the exception count. The link to the registry may
// have a neutral label, but a count in that label is a separate claim.
func TestParity_ReadmeExceptionCount(t *testing.T) {
	readme := string(mustReadRepo(t, "README.md"))
	const target = "docs/mcp.md#ответ-инструмента-и---json-команды"
	n := len(jsonAliasCompat)
	// Russian uses different cases in «Исключений семь» and «всех семи команд».
	forms := []string{"ноль|нуль|нуля", "один|одна|одно|одной|одного", "два|две|двух", "три|трёх", "четыре|четырёх", "пять|пяти", "шесть|шести", "семь|семи", "восемь|восьми", "девять|девяти", "десять|десяти"}
	if n >= len(forms) {
		t.Fatalf("%d exceptions: add the word forms for this count", n)
	}
	want := regexp.MustCompile(`^(?:` + strconv.Itoa(n) + "|" + forms[n] + `)$`)
	mainCount := regexp.MustCompile(`(?i)исключений ([0-9]+|\p{L}+)`)
	extraCount := regexp.MustCompile(`(?i)(?:кроме|за исключением) (\p{L}+|[0-9]+) (?:команд|исключени|случа)`)
	// Counts can appear after the noun or before an adjective in a link.
	// Treat a dotted version as a single token, separate from cardinal numbers.
	labelWords := regexp.MustCompile(`[\p{L}\p{N}]+(?:\.[\p{L}\p{N}]+)*`)
	numeral := regexp.MustCompile(`^(?:[0-9]+|` + strings.Join(forms, "|") + `|\p{L}+надцат[ьи])$`)
	found := false
	for bullet := range strings.SplitSeq(readme, "\n- ") {
		text := strings.Join(strings.Fields(bullet), " ")
		for _, link := range markdownLinks(text) {
			if link.raw != target {
				continue
			}
			found = true
			prose, _, _ := strings.Cut(text, "["+link.label+"]("+target+")")
			counts := mainCount.FindAllStringSubmatch(prose, -1)
			if len(counts) == 0 {
				t.Error("README: missing registered exception count in the paragraph linking to the parity table")
			}
			counts = append(counts, extraCount.FindAllStringSubmatch(prose, -1)...)
			for _, count := range counts {
				if !want.MatchString(strings.ToLower(count[1])) {
					t.Errorf("README says %q, registry has %d JSON exceptions", count[0], n)
				}
			}
			for _, word := range labelWords.FindAllString(strings.ToLower(link.label), -1) {
				if numeral.MatchString(word) && !want.MatchString(word) {
					t.Errorf("README link %q says %q, registry has %d JSON exceptions", link.label, word, n)
				}
			}
		}
	}
	if !found {
		t.Fatal("README: missing link to the JSON parity exception table")
	}
}
