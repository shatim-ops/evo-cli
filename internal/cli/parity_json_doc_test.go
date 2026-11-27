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
	forms := []string{"ноль|нуль|нуля", "один|одна|одно|одной|одного", "два|две|двух", "три|тр[её]х", "четыре|четыр[её]х", "пять|пяти", "шесть|шести", "семь|семи", "восемь|восьми", "девять|девяти", "десять|десяти"}
	if n >= len(forms) {
		t.Fatalf("%d exceptions: add the word forms for this count", n)
	}
	want := regexp.MustCompile(`^(?:` + strconv.Itoa(n) + "|" + forms[n] + `)$`)
	mainCount := regexp.MustCompile(`(?i)исключений ([0-9]+|\p{L}+)`)
	extraCount := regexp.MustCompile(`(?i)(?:кроме|за исключением) (\p{L}+|[0-9]+) (?:команд|исключени|случа)`)
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
			for _, count := range parityLabelCounts(link.label, forms) {
				if !want.MatchString(count) {
					t.Errorf("README link %q says %q, registry has %d JSON exceptions", link.label, count, n)
				}
			}
		}
	}
	if !found {
		t.Fatal("README: missing link to the JSON parity exception table")
	}
}

// A bare number labels the table itself. In a longer caption, count only
// commands, exceptions or cases. The same caption can also count interfaces.
func parityLabelCounts(label string, smallNumbers []string) []string {
	// Keep a version such as 1.9.1 together; keep punctuation between claims.
	token := regexp.MustCompile(`[\p{L}\p{N}]+(?:\.[\p{L}\p{N}]+)*|[^\p{L}\p{N}\s]`)
	words := token.FindAllString(strings.ToLower(label), -1)
	largerNumber := `\p{L}+надцат[ьи]|(?:двад|трид)цат[ьи]|сорок(?:а)?|\p{L}+десят(?:и)?|девяност[оа]|сто|ста|двести|(?:три|четыре)ста|\p{L}+сот`
	number := regexp.MustCompile(`^(?:[0-9]+|` + strings.Join(smallNumbers, "|") + "|" + largerNumber + `)$`)
	// The tokenizer separates «CLI-команд» into three tokens, including the hyphen.
	command := `(?:cli [-‐‑] )?команд(?:а|ы|у|ой|е|ам|ами|ах)?`
	subject := `(?:` + command + `|исключени(?:е|й|я|ю|ем|ям|ями|ях)|случа(?:й|и|я|е|ев|ях))`
	adjective := `(?:\p{L}+(?:ых|их|ые|ие|ая|ой|ого|его|ый|ий) )*`
	followingSubject := regexp.MustCompile(`^` + adjective + subject + `(?: |$)`)
	precedingSubject := regexp.MustCompile(`(?:^| )` + subject + `(?: (?::|—|–|-|всего|ровно|в таблице))*$`)
	// In «команд двух интерфейсов», the following noun owns the second count.
	endOfClaim := regexp.MustCompile(`^(?:$|[^\p{L}\p{N}]|(?:в|во|для|на|из|по|и|при)(?: |$))`)
	var counts []string
	for i := 0; i < len(words); {
		if !number.MatchString(words[i]) {
			i++
			continue
		}
		start := i
		i++
		for i < len(words) && number.MatchString(words[i]) {
			i++
		}
		before, after := strings.Join(words[:start], " "), strings.Join(words[i:], " ")
		bare := after == "" && (before == "" || before == "всего" || before == "все" || before == "всех")
		if bare || followingSubject.MatchString(after) || (precedingSubject.MatchString(before) && endOfClaim.MatchString(after)) {
			counts = append(counts, strings.Join(words[start:i], " "))
		}
	}
	return counts
}
