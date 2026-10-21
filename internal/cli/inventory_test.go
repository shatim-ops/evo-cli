// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// docs/internal/cli-mcp-parity/inventory.md — the inventory "tool ↔ CLI
// command" (spec.md REQ-10): generated here from the registry, the command
// tree and the lists the parity tests keep, as docs/tools.md and
// docs/commands.md are. Nothing in it is typed by hand.

const (
	inventoryDocPath        = repoRoot + "/docs/internal/cli-mcp-parity/inventory.md"
	inventoryRegenerateHint = "go test ./internal/cli -run TestParityInventory -update-docs"
	allDocsRegenerateHint   = "go test ./internal/cli -run 'TestCommandsDoc|TestToolsDoc|TestParityInventory' -update-docs"
)

// invCmd is one command of the binary, hidden ones included.
type invCmd struct {
	cmd *cobra.Command
	// tool is the tool the command calls (toolAnnotation), "" if none.
	tool string
	// hidden: hidden itself or under a hidden command.
	hidden bool
	// documented: has a section in docs/commands.md (genCommandsDoc).
	documented bool
}

// inventoryCommands walks the command tree as the binary has it, root
// excluded, in the order of the tree.
func inventoryCommands() []invCmd {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	defer i18n.SetLang(prev)
	root := newRootCmd()
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	var out []invCmd
	var walk func(c *cobra.Command, hidden, documented bool)
	walk = func(c *cobra.Command, hidden, documented bool) {
		for _, sub := range c.Commands() {
			h := hidden || sub.Hidden
			d := documented && sub.IsAvailableCommand() && !sub.IsAdditionalHelpTopicCommand()
			out = append(out, invCmd{cmd: sub, tool: sub.Annotations[toolAnnotation], hidden: h, documented: d})
			walk(sub, h, d)
		}
	}
	walk(root, false, true)
	return out
}

// specCmd is the command a tool's CLISpec names.
type specCmd struct {
	tools.CLISpec
	Tool   string
	Path   string
	Engine bool // an engine command (engineCommands): its own flags
}

// specCmds are the commands of the registry's CLISpecs, by group and verb.
func specCmds(reg *tools.Registry) []specCmd {
	var out []specCmd
	for _, t := range reg.Tools() {
		if t.CLI.Verb == "" {
			continue
		}
		_, engine := engineCommandOf(t.Name)
		out = append(out, specCmd{CLISpec: t.CLI, Tool: t.Name, Path: toolCommandPath(t), Engine: engine})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Verb < out[j].Verb
	})
	return out
}

// positionalNames are the schema arguments the command takes as its
// arguments.
func positionalNames(args []string) map[string]bool {
	pos := map[string]bool{}
	for _, p := range parsePositionals(args) {
		for _, n := range p.names {
			pos[n] = true
		}
	}
	return pos
}

// wantFlags are the flags the trees give the tool's arguments: every schema
// argument that is not positional, not omitted and not served by the root
// (rootArgs), snake_case → kebab-case or as Rename says, and the
// --<flag>-file of a SecretFiles argument beside its flag. Written here
// again, not taken from trees.go, so that the inventory checks the trees.
func wantFlags(c specCmd, t tools.Tool) map[string]string {
	pos := positionalNames(c.Args)
	want := map[string]string{} // flag → argument
	for name := range schemaProps(t) {
		if pos[name] || c.Omit[name] || (rootArgs[name] && c.Rename[name] == "") {
			continue
		}
		f := strings.ReplaceAll(name, "_", "-")
		if r, ok := c.Rename[name]; ok {
			f = r
		}
		want[f] = name
		if c.SecretFiles[name] {
			want[f+"-file"] = name // the same argument, from a file
		}
	}
	return want
}

func schemaProps(t tools.Tool) map[string]any {
	props, _ := t.InputSchema["properties"].(map[string]any)
	return props
}

func genInventory() []byte {
	reg := tools.New(nil)
	cmds := inventoryCommands()
	specs := specCmds(reg)

	byPath := map[string]invCmd{}
	documented, hidden := 1, 0 // evo itself is documented
	var others []string        // neither documented nor hidden: evo help
	for _, c := range cmds {
		byPath[c.cmd.CommandPath()] = c
		switch {
		case c.documented:
			documented++
		case c.hidden:
			hidden++
		default:
			others = append(others, "`"+c.cmd.CommandPath()+"`")
		}
	}
	aliases := map[string][]string{} // tool → alias paths
	for _, ec := range engineCommands {
		if ec.Alias != "" {
			aliases[ec.Tool] = append(aliases[ec.Tool], ec.Path)
		}
	}
	mutating := 0
	for _, t := range reg.Tools() {
		if t.Mutating {
			mutating++
		}
	}

	var w bytes.Buffer
	fmt.Fprintf(&w, `# Инвентаризация CLI ↔ MCP

<!-- Файл сгенерирован тестом TestParityInventory (internal/cli/inventory_test.go)
     из реестра инструментов, дерева команд и списков тестов паритета. Руками не править:
     %s
     Все три сгенерированных документа (docs/commands.md, docs/tools.md, этот):
     %s
     CI сверяет файл с кодом. -->

`, inventoryRegenerateHint, allDocsRegenerateHint)
	fmt.Fprintf(&w, "Инструментов в реестре: %d (изменяющих: %d). Команд из `CLISpec`: %d. "+
		"Команд в бинаре вместе с корнем `evo`: %d — в docs/commands.md %d, скрытых %d, прочих %d (%s).\n\n",
		len(reg.Tools()), mutating, len(specs), len(cmds)+1, documented, hidden, len(others), dash(strings.Join(others, ", ")))

	// --- 1. Tools by how the CLI calls them ---
	kindOf := func(t tools.Tool) string {
		if t.CLI.Verb == "" {
			return "tools call"
		}
		if _, ok := engineCommandOf(t.Name); ok {
			return "движок"
		}
		return "дерево"
	}
	kinds := []string{"дерево", "движок", "tools call"}
	kindN, kindMut := map[string]int{}, map[string]int{}
	groupKind := map[string]map[string]int{}
	for _, t := range reg.Tools() {
		k := kindOf(t)
		kindN[k]++
		if t.Mutating {
			kindMut[k]++
		}
		if groupKind[t.Group] == nil {
			groupKind[t.Group] = map[string]int{}
		}
		groupKind[t.Group][k]++
	}
	fmt.Fprintf(&w, "## 1. Инструменты по способу вызова из CLI\n\n| Способ | Инструментов | из них изменяющих |\n|---|---|---|\n")
	for _, k := range kinds {
		fmt.Fprintf(&w, "| %s | %d | %d |\n", k, kindN[k], kindMut[k])
	}
	var aliasPaths []string
	for _, ec := range engineCommands {
		if ec.Alias != "" {
			aliasPaths = append(aliasPaths, "`"+ec.Path+"`")
		}
	}
	var enginePaths []string
	for _, ec := range engineCommands {
		if ec.Alias == "" {
			enginePaths = append(enginePaths, "`"+ec.Path+"`")
		}
	}
	fmt.Fprintf(&w, "\n«дерево» — своя команда, построенная из `CLISpec` инструмента в реестре; «движок» — команда движка v1.0 (%s) "+
		"со своей справкой и флагами, которая вызывает инструмент через реестр (spec.md REQ-4; флаги сверяет TestParity_EngineCommands); "+
		"«tools call» — доступен из CLI только как `evo tools call <имя>`. ",
		strings.Join(enginePaths, ", "))
	if kindN["tools call"] == 0 {
		fmt.Fprintf(&w, "Таких нет: временный список «без команды» из PR 1 опустошён (PR 3), у каждого инструмента — своя команда, "+
			"и TestParity_CommandsAndTools не даёт добавить инструмент без неё. ")
	}
	fmt.Fprintf(&w, "Алиасы (%s) — вторые команды тех же инструментов, с причиной в `engineCommands`.\n\n", strings.Join(aliasPaths, ", "))

	fmt.Fprintf(&w, "### По группам\n\n| Группа | Всего | дерево | движок | tools call | RU-справка (short/long) |\n|---|---|---|---|---|---|\n")
	for _, g := range tools.Groups() {
		gk := groupKind[g]
		total := gk["дерево"] + gk["движок"] + gk["tools call"]
		if total == 0 {
			continue
		}
		short, long := 0, 0
		for _, t := range reg.Tools() {
			if t.Group == g {
				if t.ShortRU != "" {
					short++
				}
				if t.DescriptionRU != "" {
					long++
				}
			}
		}
		fmt.Fprintf(&w, "| %s | %d | %d | %d | %d | %d/%d |\n", g, total, gk["дерево"], gk["движок"], gk["tools call"], short, long)
	}

	// --- 2. Tool → command ---
	fmt.Fprintf(&w, "\n## 2. Инструмент → команда CLI\n\n| Группа | Инструмент | Меняет | Способ | Команда CLI | RU short/long |\n|---|---|---|---|---|---|\n")
	toolList := append([]tools.Tool(nil), reg.Tools()...)
	sort.Slice(toolList, func(i, j int) bool {
		if toolList[i].Group != toolList[j].Group {
			return toolList[i].Group < toolList[j].Group
		}
		return toolList[i].Name < toolList[j].Name
	})
	for _, t := range toolList {
		paths := []string{toolCommandCell(t)}
		for _, a := range aliases[t.Name] {
			paths = append(paths, "`"+a+"` (алиас)")
		}
		fmt.Fprintf(&w, "| %s | `%s` | %s | %s | %s | %s/%s |\n", t.Group, t.Name, yn(t.Mutating), kindOf(t),
			strings.Join(paths, ", "), yn(t.ShortRU != ""), yn(t.DescriptionRU != ""))
	}

	// --- 3. Command → tool ---
	fmt.Fprintf(&w, "\n## 3. Команда CLI → инструмент\n\nКоманды из `CLISpec`: позиционные аргументы, переименования и поля схемы без флага.\n\n"+
		"| Команда | Инструмент | Позиционные | Rename (аргумент→флаг) | Без флага (Omit) | Через корень | Pairs/Files/SecretFiles/Raw/Render |\n|---|---|---|---|---|---|---|\n")
	for _, c := range specs {
		t, _ := reg.Lookup(c.Tool)
		var omitted, viaRoot []string
		for name := range schemaProps(t) {
			if c.Omit[name] {
				omitted = append(omitted, name)
			} else if rootArgs[name] && c.Rename[name] == "" {
				viaRoot = append(viaRoot, name)
			}
		}
		sort.Strings(omitted)
		sort.Strings(viaRoot)
		var renames []string
		for k, v := range c.Rename {
			renames = append(renames, k+"→--"+v)
		}
		sort.Strings(renames)
		var extra []string
		for k := range c.Pairs {
			extra = append(extra, "pairs:"+k)
		}
		for k := range c.Files {
			extra = append(extra, "file:"+k)
		}
		for k := range c.SecretFiles {
			extra = append(extra, "secret-file:"+k)
		}
		if c.Raw {
			extra = append(extra, "raw")
		}
		if c.Render != "" {
			extra = append(extra, "render")
		}
		sort.Strings(extra)
		fmt.Fprintf(&w, "| `%s` | `%s` | %s | %s | %s | %s | %s |\n", c.Path, c.Tool,
			dash(strings.ReplaceAll(strings.Join(c.Args, " "), "|", `\|`)), dash(strings.Join(renames, ", ")),
			dash(strings.Join(omitted, ", ")), dash(strings.Join(viaRoot, ", ")), dash(strings.Join(extra, ", ")))
	}

	fmt.Fprintf(&w, "\nКоманды без своего `CLISpec`: алиасы (`engineCommands`) и команды без инструмента — закрытый список "+
		"`localCommands` (internal/cli/local_commands.go; в docs/commands.md — пометка «Локальная команда»). "+
		"Группы деревьев (`evo vm`, `evo dns`, …) сюда не входят.\n\n"+
		"| Команда | Вид | Инструмент | В docs/commands.md | Почему |\n|---|---|---|---|---|\n")
	type outside struct{ path, kind, tool, reason string }
	var rows []outside
	for _, ec := range engineCommands {
		if ec.Alias != "" {
			rows = append(rows, outside{ec.Path, "алиас", "`" + ec.Tool + "`", ec.Alias})
		}
	}
	for _, lc := range localCommands {
		rows = append(rows, outside{lc.Path, "локальная", "—", lc.Reason})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].path < rows[j].path })
	for _, r := range rows {
		inDoc := "нет"
		switch c := byPath[r.path]; {
		case c.documented:
			inDoc = "да"
		case c.hidden:
			inDoc = "нет, скрыта"
		case r.path == "evo help":
			inDoc = "нет, справка cobra"
		}
		fmt.Fprintf(&w, "| `%s` | %s | %s | %s | %s |\n", r.path, r.kind, r.tool, inDoc, cellText(r.reason))
	}

	fmt.Fprintf(&w, "\nСкрытые команды (работают, в справке и docs/commands.md их нет):\n\n| Команда | Что это | Статус |\n|---|---|---|\n")
	local := map[string]bool{}
	for _, lc := range localCommands {
		local[lc.Path] = true
	}
	aliasOf := map[string]string{}
	for _, ec := range engineCommands {
		if ec.Alias != "" {
			aliasOf[ec.Path] = ec.Tool
		}
	}
	for _, c := range cmds {
		if !c.hidden {
			continue
		}
		path := c.cmd.CommandPath()
		what := "команда без инструмента (`localCommands`)"
		switch {
		case aliasOf[path] != "":
			what = "алиас инструмента `" + aliasOf[path] + "`"
		case c.tool != "":
			what = "команда инструмента `" + c.tool + "`"
		case !local[path]:
			what = "—"
		}
		status := "служебная"
		if why, ok := deprecatedCommands[path]; ok {
			status = "устарела: " + why
		}
		fmt.Fprintf(&w, "| `%s` | %s | %s |\n", path, what, status)
	}

	// --- 4. Flags of the commands against the schema ---
	fmt.Fprintf(&w, "\n## 4. Флаги команд деревьев против схемы инструмента\n\nОжидаемый набор флагов выводится из схемы "+
		"(snake_case → kebab-case, минус позиционные, Omit и rootArgs, плюс Rename) и сравнивается с флагами команды в дереве cobra. "+
		"Пусто — расхождений нет.\n\n")
	drift, engine := 0, 0
	for _, c := range specs {
		if c.Engine {
			engine++ // their flags: engineCommands, checked by TestParity_EngineCommands
			continue
		}
		t, _ := reg.Lookup(c.Tool)
		want := wantFlags(c, t)
		got := map[string]bool{}
		if ic, ok := byPath[c.Path]; ok {
			ic.cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name != "help" {
					got[f.Name] = true
				}
			})
		}
		var missing, extra []string
		for f := range want {
			if !got[f] {
				missing = append(missing, "--"+f)
			}
		}
		for f := range got {
			if _, ok := want[f]; !ok {
				extra = append(extra, "--"+f)
			}
		}
		if len(missing)+len(extra) > 0 {
			drift++
			sort.Strings(missing)
			sort.Strings(extra)
			fmt.Fprintf(&w, "- `%s`: нет флага для %s; флаг без поля схемы %s\n", c.Path, dash(strings.Join(missing, ", ")), dash(strings.Join(extra, ", ")))
		}
	}
	if drift == 0 {
		fmt.Fprintf(&w, "Расхождений нет: флаги всех %d команд деревьев выведены из схемы; флаги %d команд движка сверяет со схемой TestParity_EngineCommands.\n",
			len(specs)-engine, engine)
	}

	// --- 5. Russian help ---
	flagsTree, flagsRU := 0, 0
	for _, c := range specs {
		if c.Engine {
			continue
		}
		t, _ := reg.Lookup(c.Tool)
		for _, name := range wantFlags(c, t) {
			if name == "confirmed" {
				continue
			}
			flagsTree++
			if t.ArgDescriptionsRU[name] != "" {
				flagsRU++
			}
		}
	}
	nShort, nLong, nArgs, argsRU := 0, 0, 0, 0
	for _, t := range reg.Tools() {
		if t.ShortRU != "" {
			nShort++
		}
		if t.DescriptionRU != "" {
			nLong++
		}
		for name := range schemaProps(t) {
			nArgs++
			if t.ArgDescriptionsRU[name] != "" {
				argsRU++
			}
		}
	}
	n := len(reg.Tools())
	fmt.Fprintf(&w, "\n## 5. Русская справка\n\n| Что | Есть по-русски | Всего |\n|---|---|---|\n")
	fmt.Fprintf(&w, "| краткое описание инструмента (ShortRU) | %d | %d инструментов |\n", nShort, n)
	fmt.Fprintf(&w, "| подробное описание инструмента (DescriptionRU) | %d | %d инструментов |\n", nLong, n)
	fmt.Fprintf(&w, "| описания аргументов (ArgDescriptionsRU) | %d | %d аргументов |\n", argsRU, nArgs)
	fmt.Fprintf(&w, "| из них флаги команд деревьев | %d | %d флагов |\n", flagsRU, flagsTree)
	fmt.Fprintf(&w, "| описания инструментов в MCP | 0 | %d (всегда английские) |\n", n)

	// --- 6. Exceptions ---
	fmt.Fprintf(&w, "\n## 6. Исключения из паритета\n\nЗакрытые списки тестов паритета: запись без причины или без команды роняет тест. "+
		"Пустой список — исключений нет.\n\n")
	var none []string
	for _, t := range toolList {
		if t.CLI.None != "" {
			none = append(none, "`"+t.Name+"`: "+t.CLI.None)
		}
	}
	fmt.Fprintf(&w, "**Инструменты без команды (`CLISpec.None`)**: %s.\n\n", listOrNone(none))
	var ruExc []string
	for _, k := range sortedStringKeys(russianHelpExceptions) {
		ruExc = append(ruExc, "`"+k+"`: "+russianHelpExceptions[k])
	}
	fmt.Fprintf(&w, "**Команды с английской справкой (`russianHelpExceptions`)**: %s.\n\n", listOrNone(ruExc))
	fmt.Fprintf(&w, "**`--json` команды не равен ответу инструмента (`jsonAliasCompat`, TestParity_JSONEqualsTool)**:\n\n| Команда | Почему |\n|---|---|\n")
	compat := make([]string, 0, len(jsonAliasCompat))
	for k := range jsonAliasCompat {
		compat = append(compat, k)
	}
	sort.Strings(compat)
	for _, k := range compat {
		fmt.Fprintf(&w, "| `%s` | %s |\n", k, cellText(jsonAliasCompat[k].reason))
	}
	fmt.Fprintf(&w, "\n**Ответ — текст, а не JSON (`textAnswer`)**:\n\n| Инструмент | Почему |\n|---|---|\n")
	for _, k := range sortedStringKeys(textAnswer) {
		fmt.Fprintf(&w, "| `%s` | %s |\n", k, cellText(textAnswer[k]))
	}
	fmt.Fprintf(&w, "\n**Функции internal/cli, которым ещё можно звать движок напрямую (`engineCallsAllowed`)**:\n\n| Функция | Почему |\n|---|---|\n")
	for _, k := range sortedStringKeys(engineCallsAllowed) {
		fmt.Fprintf(&w, "| `%s` | %s |\n", k, cellText(engineCallsAllowed[k]))
	}
	return append(bytes.TrimRight(w.Bytes(), "\n"), '\n')
}

func yn(b bool) string {
	if b {
		return "да"
	}
	return "нет"
}

// cellText is s on one line, safe inside a Markdown table cell.
func cellText(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), "|", `\|`)
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "нет"
	}
	return strings.Join(items, "; ")
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestParityInventory(t *testing.T) {
	got := genInventory()
	if *updateDocs {
		if err := os.WriteFile(inventoryDocPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(inventoryDocPath)
	if err != nil {
		t.Fatalf("%v; generate it: %s", err, inventoryRegenerateHint)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Fatalf("docs/internal/cli-mcp-parity/inventory.md differs from the code (first difference at line %d); regenerate: %s",
			firstDiffLine(got, want), inventoryRegenerateHint)
	}
}

// The inventory sees the tree TestParity_CommandsAndTools checks: every
// command of a CLISpec is in it, and no flag of a tree command drifts from
// its schema (§4 reports a drift; this makes it a failure).
func TestParityInventory_Consistent(t *testing.T) {
	reg := tools.New(nil)
	byPath := map[string]invCmd{}
	for _, c := range inventoryCommands() {
		byPath[c.cmd.CommandPath()] = c
	}
	for _, c := range specCmds(reg) {
		ic, ok := byPath[c.Path]
		if !ok {
			t.Errorf("%s: CLISpec of %s names no command of the tree", c.Path, c.Tool)
			continue
		}
		if ic.tool != c.Tool {
			t.Errorf("%s: calls %q, CLISpec of %s names it", c.Path, ic.tool, c.Tool)
		}
	}
	if bytes.Contains(genInventory(), []byte("\n- `evo ")) {
		t.Error("inventory §4 reports flags drifting from the schema")
	}
}
