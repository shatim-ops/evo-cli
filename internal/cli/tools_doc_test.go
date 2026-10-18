// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// docs/tools.md — каталог инструментов, сгенерированный из реестра, как
// docs/commands.md из дерева команд: 49 из 152 инструментов не описывал ни
// один документ (релизный аудит, A-007).

const (
	toolsDocPath        = repoRoot + "/docs/tools.md"
	toolsRegenerateHint = "go test ./internal/cli -run TestToolsDoc -update-docs"
)

// genToolsDoc renders the registry: per group, every tool with whether it
// changes anything, its required arguments and the first sentence of its
// description (the MCP description, in English).
func genToolsDoc() []byte {
	reg := newRegistry()
	byGroup := map[string][]tools.Tool{}
	for _, t := range reg.Tools() {
		byGroup[t.Group] = append(byGroup[t.Group], t)
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `# Каталог инструментов

<!-- Файл сгенерирован из реестра инструментов. Руками не править:
     %s
     CI сверяет файл с реестром (TestToolsDoc). -->

Инструменты evo (%d) — те же, что у MCP-сервера ([mcp.md](mcp.md)) и у
`+"`evo tools call`"+`. Здесь — первая фраза описания; полное описание и схема
аргументов — `+"`evo tools describe <имя>`"+`. «Меняет»: «да» — инструмент
создаёт или меняет ресурсы и без `+"`confirmed=true`"+` (в CLI — `+"`--yes`"+`)
только показывает превью; «удаляет» — может удалить или перезаписать данные.
Группы — для `+"`evo mcp --toolsets`"+` и `+"`evo tools list --group`"+`.

`, toolsRegenerateHint, len(reg.Tools()))
	for _, g := range tools.Groups() {
		list := byGroup[g]
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "## %s\n\n| Инструмент | Меняет | Обязательные аргументы | Описание |\n|---|---|---|---|\n", g)
		for _, t := range list {
			changes := "нет"
			switch {
			case tools.IsDestructive(t):
				changes = "удаляет"
			case t.Mutating:
				changes = "да"
			}
			req := "—"
			if names := requiredArgs(t); len(names) > 0 {
				req = "`" + strings.Join(names, "`, `") + "`"
			}
			fmt.Fprintf(&buf, "| `%s` | %s | %s | %s |\n", t.Name, changes, req, firstSentence(t.Description))
		}
		buf.WriteString("\n")
	}
	return append(bytes.TrimRight(buf.Bytes(), "\n"), '\n')
}

// requiredArgs lists the "required" of the tool's input schema.
func requiredArgs(t tools.Tool) []string {
	var out []string
	switch r := t.InputSchema["required"].(type) {
	case []string:
		out = append(out, r...)
	case []any:
		for _, x := range r {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// firstSentence is the description up to its first ". ", on one line, at
// most about 160 characters (cut at a word), safe inside a Markdown table
// cell.
func firstSentence(d string) string {
	d = strings.Join(strings.Fields(d), " ")
	if i := strings.Index(d, ". "); i >= 0 {
		d = d[:i+1]
	}
	if r := []rune(d); len(r) > 160 {
		cut := string(r[:160])
		if i := strings.LastIndex(cut, " "); i > 80 {
			cut = cut[:i]
		}
		d = strings.TrimRight(cut, " ,;:(") + " …"
	}
	return strings.ReplaceAll(d, "|", `\|`)
}

func TestToolsDoc(t *testing.T) {
	got := genToolsDoc()
	if *updateDocs {
		if err := os.WriteFile(toolsDocPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(toolsDocPath)
	if err != nil {
		t.Fatalf("%v; generate it: %s", err, toolsRegenerateHint)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Fatalf("docs/tools.md differs from the tool registry (first difference at line %d); regenerate: %s",
			firstDiffLine(got, want), toolsRegenerateHint)
	}
	// Каждый инструмент — в каталоге.
	for _, tl := range newRegistry().Tools() {
		if !bytes.Contains(got, []byte("| `"+tl.Name+"` |")) {
			t.Errorf("%s is not in docs/tools.md", tl.Name)
		}
	}
}
