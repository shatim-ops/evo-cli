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

// genToolsDoc renders the registry: per group, every tool with its command
// (CLISpec), whether it changes anything, its required arguments, the
// first sentence of its description (the MCP description, in English) and
// its Russian summary (ShortRU).
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
`+"`evo tools call`"+`. «Команда CLI» — команда инструмента в evo
([commands.md](commands.md)). «Описание» — первая фраза английского описания,
которое читает агент в MCP; «Описание (RU)» — краткое русское, из справки
команды. Полное описание и схема аргументов — `+"`evo tools describe <имя>`"+`
(с `+"`EVO_LANG=en`"+` — по-английски). «Меняет»: «да» — инструмент
создаёт или меняет ресурсы и без `+"`confirmed=true`"+` (в CLI — `+"`--yes`"+`)
только показывает превью; «удаляет» — может удалить или перезаписать данные;
«нет, секрет» — ничего не меняет, но показывает секрет открытым текстом и
тоже требует `+"`confirmed=true`"+` (`+"`--yes`"+`).
Группы — для `+"`evo mcp --toolsets`"+` и `+"`evo tools list --group`"+`.

`, toolsRegenerateHint, len(reg.Tools()))
	for _, g := range tools.Groups() {
		list := byGroup[g]
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "## %s\n\n| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |\n|---|---|---|---|---|---|\n", g)
		for _, t := range list {
			changes := "нет"
			switch {
			case tools.IsDestructive(t):
				changes = "удаляет"
			case t.Mutating:
				changes = "да"
			case tools.IsSensitive(t):
				changes = "нет, секрет"
			}
			var reqs []string
			for _, n := range requiredArgs(t) {
				reqs = append(reqs, "`"+n+"`")
			}
			// One of a set (vm_get: vm_id or name): "`vm_id` или `name`".
			for _, set := range t.RequiredOneOf {
				reqs = append(reqs, "`"+strings.Join(set, "` или `")+"`")
			}
			req := "—"
			if len(reqs) > 0 {
				req = strings.Join(reqs, ", ")
			}
			fmt.Fprintf(&buf, "| `%s` | %s | %s | %s | %s | %s |\n", t.Name, toolCommandCell(t), changes, req,
				firstSentence(t.Description), russianSummary(t))
		}
		buf.WriteString("\n")
	}
	return append(bytes.TrimRight(buf.Bytes(), "\n"), '\n')
}

// toolCommandPath is the command CLISpec gives the tool ("evo vm create";
// Group "": a command at the root), "" for a tool without one.
func toolCommandPath(t tools.Tool) string {
	if t.CLI.Verb == "" {
		return ""
	}
	return strings.Join(strings.Fields("evo "+t.CLI.Group+" "+t.CLI.Verb), " ")
}

// toolCommandCell is the tool's command for a table cell; a tool without
// one is called with 'evo tools call'.
func toolCommandCell(t tools.Tool) string {
	if p := toolCommandPath(t); p != "" {
		return "`" + p + "`"
	}
	return "`evo tools call " + t.Name + "`"
}

// russianSummary is the Russian summary of the tool (ShortRU; else the
// first sentence of DescriptionRU), for a table cell.
func russianSummary(t tools.Tool) string {
	if t.ShortRU != "" {
		return strings.ReplaceAll(strings.Join(strings.Fields(t.ShortRU), " "), "|", `\|`)
	}
	if t.DescriptionRU != "" {
		return firstSentence(t.DescriptionRU)
	}
	return "—"
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
