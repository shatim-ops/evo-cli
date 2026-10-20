// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build ignore

// Инвентаризация «инструмент ↔ команда CLI» для docs/internal/cli-mcp-parity.
//
// Не продуктовый код: файл исключён из сборки (//go:build ignore) и
// запускается вручную из корня репозитория:
//
//	go run ./docs/internal/cli-mcp-parity/inventory/main.go > docs/internal/cli-mcp-parity/inventory.md
//
// Источники (ничего не вводится руками, кроме таблицы localCommands —
// классификации команд CLI, у которых нет инструмента; она проверяется
// против docs/commands.md: команда без строки в ней или строка без команды
// — ошибка):
//
//   - реестр инструментов: internal/tools (tools.New(nil) — без облака);
//   - деревья команд: CLISpec инструментов реестра (Tool.CLI: группа, глагол,
//     позиционные аргументы, Rename/Omit/Pairs/Files/Raw/Render) и временный
//     список tools.CLIPending — инструменты, у которых команды пока нет;
//   - rootArgs (аргументы, которые подаются корневыми флагами): AST-разбор
//     internal/cli/trees.go;
//   - русская справка деревьев: ключи карт toolShortRU, toolLongRU,
//     flagShortRU в internal/cli/trees_ru.go;
//   - фактический набор команд и флагов бинаря: docs/commands.md (его CI
//     сверяет с бинарём тестом TestCommandsDoc).
//
// Выход — Markdown: сводка, таблица «инструмент → команда», таблица
// «команда → инструмент», расхождения флагов деревьев со схемой (ожидается
// пусто) и список полей схемы, которым флаг не достался (Omit/rootArgs).
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// treeCmd — команда дерева, как её описывает CLISpec инструмента.
type treeCmd struct {
	Group, Verb, Tool string
	Args              []string
	Rename            map[string]string
	Omit, Pairs       map[string]bool
	Files             map[string]bool
	Render, Raw       bool
}

// rootArgs повторяет internal/cli/trees.go: аргументы инструмента, которые
// подаются корневыми флагами (--project-id, --region) и согласием (--yes).
// Вычитывается из trees.go тоже, чтобы не разойтись.
var rootArgs = map[string]bool{}

// localCommand — команда CLI без своей команды дерева: путь движка к тому же
// инструменту, локальная команда без инструмента или мета-команда.
type localCommand struct {
	Kind   string // engine | local | meta | group | hidden
	Tool   string // инструмент, которому команда соответствует по смыслу ("" — нет)
	Reason string
}

// localCommands — ручная классификация (единственная ручная часть). Каждая
// команда из docs/commands.md, не являющаяся командой дерева, обязана быть
// здесь, иначе скрипт завершается ошибкой.
var localCommands = map[string]localCommand{
	"evo":                       {Kind: "group", Reason: "корень"},
	"evo completion":            {Kind: "local", Reason: "скрипт автодополнения оболочки; к облаку не обращается"},
	"evo completion bash":       {Kind: "local", Reason: "то же"},
	"evo completion fish":       {Kind: "local", Reason: "то же"},
	"evo completion powershell": {Kind: "local", Reason: "то же"},
	"evo completion zsh":        {Kind: "local", Reason: "то же"},
	"evo configure":             {Kind: "local", Reason: "пишет ключ в профиль на диске сервера/машины; MCP-клиент ключей не передаёт по контракту (docs/mcp.md)"},
	"evo configure delete":      {Kind: "local", Reason: "удаляет профиль с диска"},
	"evo configure list":        {Kind: "local", Reason: "читает профили с диска"},
	"evo deploy":                {Kind: "engine", Tool: "deploy", Reason: "свой код cobra → engine.Deploy; инструмент deploy — тот же engine.Deploy с NonInteractive+Yes"},
	"evo destroy":               {Kind: "engine", Tool: "destroy", Reason: "свой код → engine.Destroy (запись state); инструмент destroy без аргументов — тот же путь; с vm_id|name — команда дерева vm delete"},
	"evo doctor":                {Kind: "local", Reason: "диагностика машины: файлы настроек, права, PATH; часть (связь с API, ключ) могла бы быть инструментом"},
	"evo init":                  {Kind: "local", Reason: "пишет .evo.yaml в текущий каталог после интерактивного выбора; инструмента нет"},
	"evo list-zones":            {Kind: "engine", Tool: "list_zones", Reason: "свой код → Cloud.ListAvailabilityZones; дубль evo vm zones с другим форматом --json"},
	"evo logs":                  {Kind: "engine", Tool: "logs", Reason: "свой код → engine.Logs; у инструмента нет follow"},
	"evo mcp":                   {Kind: "meta", Reason: "сам сервер MCP"},
	"evo project":               {Kind: "group", Reason: "группа"},
	"evo project list":          {Kind: "local", Tool: "org_projects", Reason: "свой код listProjects (configure.go) → Organization API; по смыслу = org_projects, формат ответа другой"},
	"evo project use":           {Kind: "local", Reason: "пишет проект в профиль на диске"},
	"evo status":                {Kind: "engine", Tool: "status", Reason: "свой код → engine.Status; инструмент status читает state и CheckCloud сам, без withNames"},
	"evo tools":                 {Kind: "meta", Reason: "группа"},
	"evo tools call":            {Kind: "meta", Reason: "универсальный вызов любого инструмента"},
	"evo tools describe":        {Kind: "meta", Reason: "описание инструмента (англ.)"},
	"evo tools list":            {Kind: "meta", Reason: "список инструментов"},
	"evo verify":                {Kind: "local", Reason: "TCP/HTTP-проверки портов развёрнутого приложения с машины оператора; инструмента нет"},
	"evo version":               {Kind: "local", Reason: "версия бинаря; MCP отдаёт её в initialize.serverInfo"},
	"evo whoami":                {Kind: "local", Reason: "профиль, маскированный Key ID, проект, срок токена; инструмента нет"},
	// Скрытые команды (в docs/commands.md их нет): учтены для полноты.
	"evo list-images":     {Kind: "hidden", Tool: "list_images", Reason: "устаревшая, скрытая; свой код engine.ListImages (общий каталог), инструмент list_images добавляет образы проекта"},
	"evo crew":            {Kind: "hidden", Reason: "титры"},
	"evo mcp healthcheck": {Kind: "hidden", Reason: "HEALTHCHECK Docker-образа"},
}

var hiddenCommands = []string{"evo list-images", "evo crew", "evo mcp healthcheck"}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	reg := tools.New(nil)
	check(parseRootArgs(root + "/internal/cli/trees.go"))
	cmds := treeCmds(reg)
	ruShort, ruLong, ruFlags, err := parseRU(root + "/internal/cli/trees_ru.go")
	check(err)
	docCmds, docFlags, err := parseCommandsDoc(root + "/docs/commands.md")
	check(err)

	byTool := map[string][]treeCmd{}
	byPath := map[string]treeCmd{}
	for _, c := range cmds {
		byTool[c.Tool] = append(byTool[c.Tool], c)
		byPath["evo "+c.Group+" "+c.Verb] = c
	}
	engineTool := map[string][]string{}
	for path, lc := range localCommands {
		if lc.Tool != "" {
			engineTool[lc.Tool] = append(engineTool[lc.Tool], path)
		}
	}
	for _, paths := range engineTool {
		sort.Strings(paths)
	}

	// Проверки полноты ручной таблицы.
	var problems []string
	for _, path := range docCmds {
		if _, tree := byPath[path]; tree {
			continue
		}
		if _, ok := localCommands[path]; !ok {
			if _, group := byGroupPath(cmds, path); !group {
				problems = append(problems, "команда из docs/commands.md не классифицирована: "+path)
			}
		}
	}
	docSet := map[string]bool{}
	for _, p := range docCmds {
		docSet[p] = true
	}
	for path, lc := range localCommands {
		if lc.Kind == "hidden" {
			continue
		}
		if !docSet[path] {
			problems = append(problems, "localCommands называет команду, которой нет в docs/commands.md: "+path)
		}
		if lc.Tool != "" {
			if _, ok := reg.Lookup(lc.Tool); !ok {
				problems = append(problems, "localCommands называет несуществующий инструмент: "+lc.Tool)
			}
		}
	}
	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, strings.Join(problems, "\n"))
		os.Exit(1)
	}

	// --- Сводка ---
	kinds := map[string]int{}
	kindMut := map[string]int{}
	groupKind := map[string]map[string]int{}
	type toolRow struct {
		t    tools.Tool
		kind string
		path string
	}
	var toolRows []toolRow
	for _, t := range reg.Tools() {
		kind, path := "tools call", "`evo tools call "+t.Name+"`"
		switch {
		case len(byTool[t.Name]) > 0:
			kind = "дерево"
			var ps []string
			for _, c := range byTool[t.Name] {
				ps = append(ps, "`evo "+c.Group+" "+c.Verb+"`")
			}
			if len(engineTool[t.Name]) > 0 {
				for _, p := range engineTool[t.Name] {
					ps = append(ps, "`"+p+"` (движок)")
				}
			}
			path = strings.Join(ps, ", ")
		case len(engineTool[t.Name]) > 0:
			kind = "движок"
			var ps []string
			for _, p := range engineTool[t.Name] {
				ps = append(ps, "`"+p+"`")
			}
			path = strings.Join(ps, ", ") + " (свой код), иначе `evo tools call " + t.Name + "`"
		}
		kinds[kind]++
		if t.Mutating {
			kindMut[kind]++
		}
		if groupKind[t.Group] == nil {
			groupKind[t.Group] = map[string]int{}
		}
		groupKind[t.Group][kind]++
		toolRows = append(toolRows, toolRow{t, kind, path})
	}
	sort.Slice(toolRows, func(i, j int) bool {
		if toolRows[i].t.Group != toolRows[j].t.Group {
			return toolRows[i].t.Group < toolRows[j].t.Group
		}
		return toolRows[i].t.Name < toolRows[j].t.Name
	})

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprintf(w, "# Инвентаризация CLI ↔ MCP\n\n<!-- Сгенерировано: go run ./docs/internal/cli-mcp-parity/inventory/main.go > docs/internal/cli-mcp-parity/inventory.md. Руками не править. -->\n\n")
	fmt.Fprintf(w, "Инструментов в реестре: %d (изменяющих: %d). Команд дерева: %d. Команд в docs/commands.md: %d (+%d скрытых).\n\n", len(reg.Tools()), countMut(reg), len(cmds), len(docCmds), len(hiddenCommands))
	fmt.Fprintf(w, "## 1. Инструменты по способу вызова из CLI\n\n| Способ | Инструментов | из них изменяющих |\n|---|---|---|\n")
	for _, k := range []string{"дерево", "движок", "tools call"} {
		fmt.Fprintf(w, "| %s | %d | %d |\n", k, kinds[k], kindMut[k])
	}
	fmt.Fprintf(w, "\n«дерево» — своя команда, построенная из CLISpec инструмента в реестре; «движок» — команда верхнего уровня со своим кодом cobra, которая делает то же через engine, а не через реестр; «tools call» — доступен из CLI только как `evo tools call <имя>`. «движок» и «tools call» вместе — временный список `tools.CLIPending` (%d): тест TestParity_CommandsAndTools не даёт ему устареть, PR 3 и 5 плана его опустошают.\n\n", len(tools.CLIPending))
	fmt.Fprintf(w, "### По группам\n\n| Группа | Всего | дерево | движок | tools call | RU-справка (short/long) |\n|---|---|---|---|---|---|\n")
	for _, g := range tools.Groups() {
		gk := groupKind[g]
		total := gk["дерево"] + gk["движок"] + gk["tools call"]
		if total == 0 {
			continue
		}
		short, long := 0, 0
		for _, t := range reg.Tools() {
			if t.Group == g {
				if ruShort[t.Name] {
					short++
				}
				if ruLong[t.Name] {
					long++
				}
			}
		}
		fmt.Fprintf(w, "| %s | %d | %d | %d | %d | %d/%d |\n", g, total, gk["дерево"], gk["движок"], gk["tools call"], short, long)
	}

	fmt.Fprintf(w, "\n## 2. Инструмент → команда CLI\n\n| Группа | Инструмент | Меняет | Способ | Команда CLI | RU short/long |\n|---|---|---|---|---|---|\n")
	for _, r := range toolRows {
		fmt.Fprintf(w, "| %s | `%s` | %s | %s | %s | %s/%s |\n", r.t.Group, r.t.Name, yn(r.t.Mutating), r.kind, r.path, yn(ruShort[r.t.Name]), yn(ruLong[r.t.Name]))
	}

	fmt.Fprintf(w, "\n## 3. Команда CLI → инструмент\n\nКоманды деревьев (из `CLISpec`): позиционные аргументы, переименования и поля схемы без флага.\n\n| Команда | Инструмент | Позиционные | Rename (аргумент→флаг) | Без флага (Omit) | Через корень | Pairs/Files/Raw/Render |\n|---|---|---|---|---|---|---|\n")
	for _, c := range cmds {
		t, _ := reg.Lookup(c.Tool)
		props := schemaProps(t)
		var omitted, viaRoot []string
		for name := range props {
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
		if c.Raw {
			extra = append(extra, "raw")
		}
		if c.Render {
			extra = append(extra, "render")
		}
		sort.Strings(extra)
		fmt.Fprintf(w, "| `evo %s %s` | `%s` | %s | %s | %s | %s | %s |\n", c.Group, c.Verb, c.Tool,
			dash(strings.ReplaceAll(strings.Join(c.Args, " "), "|", `\|`)), dash(strings.Join(renames, ", ")), dash(strings.Join(omitted, ", ")), dash(strings.Join(viaRoot, ", ")), dash(strings.Join(extra, ", ")))
	}

	fmt.Fprintf(w, "\nКоманды вне деревьев (из docs/commands.md и скрытые):\n\n| Команда | Вид | Инструмент по смыслу | Почему |\n|---|---|---|---|\n")
	var paths []string
	for p := range localCommands {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		lc := localCommands[p]
		fmt.Fprintf(w, "| `%s` | %s | %s | %s |\n", p, lc.Kind, dash(code(lc.Tool)), lc.Reason)
	}

	// --- Сверка флагов деревьев со схемой ---
	fmt.Fprintf(w, "\n## 4. Флаги команд деревьев против схемы инструмента\n\nОжидаемый набор флагов выводится из схемы по правилам trees.go (snake_case → kebab-case, минус позиционные, Omit и rootArgs, плюс Rename) и сравнивается с docs/commands.md. Пусто — расхождений нет.\n\n")
	drift := 0
	for _, c := range cmds {
		t, _ := reg.Lookup(c.Tool)
		want := map[string]bool{}
		pos := map[string]bool{}
		for _, a := range c.Args {
			a = strings.TrimSuffix(strings.TrimSuffix(a, "..."), "?")
			for _, n := range strings.Split(a, "|") {
				pos[n] = true
			}
		}
		for name := range schemaProps(t) {
			if name == "confirmed" || pos[name] || c.Omit[name] || (rootArgs[name] && c.Rename[name] == "") {
				continue
			}
			f := strings.ReplaceAll(name, "_", "-")
			if r, ok := c.Rename[name]; ok {
				f = r
			}
			want[f] = true
		}
		got := docFlags["evo "+c.Group+" "+c.Verb]
		var missing, extra []string
		for f := range want {
			if !got[f] {
				missing = append(missing, "--"+f)
			}
		}
		for f := range got {
			if f != "help" && !want[f] {
				extra = append(extra, "--"+f)
			}
		}
		if len(missing)+len(extra) > 0 {
			drift++
			sort.Strings(missing)
			sort.Strings(extra)
			fmt.Fprintf(w, "- `evo %s %s`: нет флага для %s; флаг без поля схемы %s\n", c.Group, c.Verb, dash(strings.Join(missing, ", ")), dash(strings.Join(extra, ", ")))
		}
	}
	if drift == 0 {
		fmt.Fprintf(w, "Расхождений нет: флаги всех %d команд деревьев выведены из схемы.\n", len(cmds))
	}

	// --- RU-покрытие флагов ---
	fmt.Fprintf(w, "\n## 5. Русская справка\n\n")
	flagsTree, flagsRU := 0, 0
	for _, c := range cmds {
		t, _ := reg.Lookup(c.Tool)
		pos := map[string]bool{}
		for _, a := range c.Args {
			a = strings.TrimSuffix(strings.TrimSuffix(a, "..."), "?")
			for _, n := range strings.Split(a, "|") {
				pos[n] = true
			}
		}
		for name := range schemaProps(t) {
			if name == "confirmed" || pos[name] || c.Omit[name] || (rootArgs[name] && c.Rename[name] == "") {
				continue
			}
			flagsTree++
			if ruFlags[c.Tool+"."+name] {
				flagsRU++
			}
		}
	}
	nShort, nLong := 0, 0
	for _, t := range reg.Tools() {
		if ruShort[t.Name] {
			nShort++
		}
		if ruLong[t.Name] {
			nLong++
		}
	}
	fmt.Fprintf(w, "| Что | Есть по-русски | Всего |\n|---|---|---|\n")
	fmt.Fprintf(w, "| краткое описание инструмента (toolShortRU) | %d | %d инструментов |\n", nShort, len(reg.Tools()))
	fmt.Fprintf(w, "| подробная справка инструмента (toolLongRU) | %d | %d инструментов |\n", nLong, len(reg.Tools()))
	fmt.Fprintf(w, "| описания флагов команд деревьев (flagShortRU) | %d | %d флагов |\n", flagsRU, flagsTree)
	fmt.Fprintf(w, "| описания инструментов в MCP и `evo tools describe` | 0 | %d (всегда английские) |\n", len(reg.Tools()))
}

func countMut(reg *tools.Registry) int {
	n := 0
	for _, t := range reg.Tools() {
		if t.Mutating {
			n++
		}
	}
	return n
}

func byGroupPath(cmds []treeCmd, path string) (string, bool) {
	for _, c := range cmds {
		if "evo "+c.Group == path {
			return c.Group, true
		}
	}
	return "", false
}

func schemaProps(t tools.Tool) map[string]any {
	props, _ := t.InputSchema["properties"].(map[string]any)
	return props
}

func yn(b bool) string {
	if b {
		return "да"
	}
	return "нет"
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func code(s string) string {
	if s == "" {
		return ""
	}
	return "`" + s + "`"
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// --- Деревья: CLISpec реестра; rootArgs: AST trees.go ---

// treeCmds — команды деревьев из CLISpec инструментов, по группе и глаголу.
func treeCmds(reg *tools.Registry) []treeCmd {
	var out []treeCmd
	for _, t := range reg.Tools() {
		spec := t.CLI
		if spec.Verb == "" {
			continue
		}
		c := treeCmd{Group: spec.Group, Verb: spec.Verb, Tool: t.Name, Args: spec.Args,
			Rename: map[string]string{}, Omit: map[string]bool{}, Pairs: map[string]bool{}, Files: map[string]bool{},
			Render: spec.Render != "", Raw: spec.Raw}
		for k, v := range spec.Rename {
			c.Rename[k] = v
		}
		for k, v := range spec.Omit {
			c.Omit[k] = v
		}
		for k, v := range spec.Pairs {
			c.Pairs[k] = v
		}
		for k, v := range spec.Files {
			c.Files[k] = v
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Verb < out[j].Verb
	})
	return out
}

// parseRootArgs читает литерал rootArgs из trees.go.
func parseRootArgs(path string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return err
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i >= len(vs.Values) || name.Name != "rootArgs" {
					continue
				}
				cl, _ := vs.Values[i].(*ast.CompositeLit)
				for _, e := range cl.Elts {
					kv := e.(*ast.KeyValueExpr)
					rootArgs[lit(kv.Key)] = true
				}
			}
		}
	}
	if len(rootArgs) == 0 {
		return fmt.Errorf("%s: rootArgs not found", path)
	}
	return nil
}

// lit — строковый литерал, идентификатор или bool как текст.
func lit(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		s, err := strconv.Unquote(x.Value)
		if err != nil {
			return x.Value
		}
		return s
	case *ast.Ident:
		return x.Name
	}
	return ""
}

// --- AST: ключи карт русской справки ---

func parseRU(path string) (short, long, flags map[string]bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	keys := func(name string) map[string]bool {
		m := map[string]bool{}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if n.Name != name || i >= len(vs.Values) {
						continue
					}
					cl, _ := vs.Values[i].(*ast.CompositeLit)
					for _, e := range cl.Elts {
						m[lit(e.(*ast.KeyValueExpr).Key)] = true
					}
				}
			}
		}
		return m
	}
	return keys("toolShortRU"), keys("toolLongRU"), keys("flagShortRU"), nil
}

// --- docs/commands.md: команды и их локальные флаги ---

var (
	sectionRE = regexp.MustCompile(`^## (evo.*)$`)
	flagRE    = regexp.MustCompile(`^\s*(?:-\w, )?--([\w-]+)`)
)

func parseCommandsDoc(path string) ([]string, map[string]map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var cmds []string
	flags := map[string]map[string]bool{}
	cur, inOptions, inCode := "", false, false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if m := sectionRE.FindStringSubmatch(line); m != nil {
			cur = m[1]
			cmds = append(cmds, cur)
			flags[cur] = map[string]bool{}
			inOptions = false
			continue
		}
		switch {
		case line == "### Options":
			inOptions, inCode = true, false
		case strings.HasPrefix(line, "### "):
			inOptions = false
		case inOptions && strings.HasPrefix(line, "```"):
			if inCode {
				inOptions = false
			}
			inCode = !inCode
		case inOptions && inCode:
			if m := flagRE.FindStringSubmatch(line); m != nil {
				flags[cur][m[1]] = true
			}
		}
	}
	return cmds, flags, sc.Err()
}
