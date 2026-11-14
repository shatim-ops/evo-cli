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
// (CLISpec), what it does besides answering (tools.EffectOf), its required arguments, the
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

Инструменты evo — те же, что у MCP-сервера ([mcp.md](mcp.md)) и у
`+"`evo tools call`"+`. «Команда CLI» — команда инструмента в evo
([commands.md](commands.md)). «Описание» — первая фраза английского описания,
которое читает агент в MCP; «Описание (RU)» — краткое русское, из справки
команды. Полное описание и схема аргументов — `+"`evo tools describe <имя>`"+`
(с `+"`EVO_LANG=en`"+` — по-английски). «Меняет»: «да» — инструмент
создаёт или меняет ресурсы и без `+"`confirmed=true`"+` (в CLI — `+"`--yes`"+`)
только показывает превью; «удаляет» — может удалить или перезаписать данные;
«нет, секрет» — ничего не меняет, но показывает секрет открытым текстом и
тоже требует `+"`confirmed=true`"+` (`+"`--yes`"+`); «побочно» — согласия не
просит, но это не чтение: кроме ответа вызов делает ещё одно действие
(продвигает стек, выпускает токен кластера); какое — говорит карточка группы
(`+"`agent_skill`"+`), в MCP у такого инструмента нет `+"`readOnlyHint`"+`; «нет» —
только чтение.
Группы — для `+"`evo mcp --toolsets`"+` и `+"`evo tools list --group`"+`.

`, toolsRegenerateHint)
	for _, g := range tools.Groups() {
		list := byGroup[g]
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "## %s\n\n| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |\n|---|---|---|---|---|---|\n", g)
		for _, t := range list {
			changes := "нет"
			switch tools.EffectOf(t) {
			case tools.EffectDestructive:
				changes = "удаляет"
			case tools.EffectMutating:
				changes = "да"
			case tools.EffectSecret:
				changes = "нет, секрет"
			case tools.EffectSideEffects:
				changes = "побочно"
			}
			var reqs []string
			for _, n := range requiredArgs(t) {
				reqs = append(reqs, "`"+n+"`")
			}
			// One of a set (vm_get: vm_id or name): "`vm_id` или `name`".
			for _, set := range t.Decl.RequiredOneOf {
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

// «Меняет» says «нет» of a read only: a tool that takes no consent but is
// not a read (tools.EffectSideEffects: stack_status advances the stack,
// k8s_kubeconfig mints a token) has no readOnlyHint in MCP, and the
// catalog does not call it a read either (external review, #1744). The
// column and the hints take one decision, tools.EffectOf.
func TestToolsDoc_NoMeansARead(t *testing.T) {
	cells := map[string]string{}
	for _, line := range strings.Split(string(genToolsDoc()), "\n") {
		f := strings.Split(line, " | ")
		if len(f) < 3 || !strings.HasPrefix(f[0], "| `") {
			continue
		}
		cells[strings.Trim(f[0], "| `")] = f[2]
	}
	for _, tl := range newRegistry().Tools() {
		cell, ok := cells[tl.Name]
		if !ok {
			t.Errorf("%s: no row in the catalog", tl.Name)
			continue
		}
		if read := tools.EffectOf(tl) == tools.EffectRead; (cell == "нет") != read {
			t.Errorf("%s: «Меняет» is %q, a read: %v", tl.Name, cell, read)
		}
	}
}
