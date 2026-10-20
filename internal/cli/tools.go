// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// newRegistry builds the tool registry with the same engine factory as the
// MCP server: credentials, profile, project and region resolve exactly as
// for every other command.
func newRegistry() *tools.Registry {
	return tools.New(func(overrides map[string]any) (*engine.Engine, error) {
		if overrides == nil {
			overrides = map[string]any{}
		}
		return toolEngine(overrides)
	})
}

// toolEngine builds the engines of the tools and of their previews
// (tests replace it to reach a fake cloud).
var toolEngine = buildEngine

// cmdPrinter is output.New writing to the command's streams (tests replace
// them with buffers).
func cmdPrinter(cmd *cobra.Command) *output.Printer {
	if cmd.OutOrStdout() == os.Stdout && cmd.ErrOrStderr() == os.Stderr {
		return output.New(flagJSON, flagNoColor)
	}
	mode := output.ModePlain
	if flagJSON {
		mode = output.ModeJSON
	}
	return output.NewWithWriters(cmd.OutOrStdout(), cmd.ErrOrStderr(), mode, false)
}

func newToolsCmd() *cobra.Command {
	cmd := asGroup(&cobra.Command{
		Use: "tools",
		Short: i18n.T("List, describe and call any of the evo tools (the same ones the MCP server exposes)",
			"Список, описание и вызов инструментов evo (тех же, что у MCP-сервера)"),
		Long: i18n.T(`Every operation the MCP server exposes is available here as a tool.

  evo tools list [--group vm]
  evo tools describe vm_get
  evo tools call vm_get --arg name=web

Tools that create paid resources or delete data ask for confirmation the same
way as the other commands: a prompt on a terminal, --yes to confirm in
advance, --dry-run to see the preview only; without a terminal and without
--yes the call stops with exit code 3.`, `Любая операция MCP-сервера доступна здесь как инструмент.

  evo tools list [--group vm]
  evo tools describe vm_get
  evo tools call vm_get --arg name=web

Инструменты, которые создают платные ресурсы или удаляют данные, спрашивают
подтверждение так же, как остальные команды: вопрос на терминале, --yes —
согласие заранее, --dry-run — только превью; без терминала и без --yes вызов
завершается с кодом 3. Описания инструментов — по-русски (с EVO_LANG=en — по-английски,
как их видит агент в MCP); ответы инструментов — на английском, как в MCP.`),
	})
	cmd.AddCommand(newToolsListCmd(), newToolsDescribeCmd(), newToolsCallCmd())
	return cmd
}

// --- tools list ---

// toolInfo is the JSON form of a tool in 'tools list' and 'tools describe'.
type toolInfo struct {
	Name        string         `json:"name"`
	Group       string         `json:"group"`
	Mutating    bool           `json:"mutating"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	// line is the 'tools list' table's description (toolSummary); not
	// in the JSON.
	line string
}

func newToolsListCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use: "list",
		Short: i18n.T("List the tools: name, group, whether it changes anything, description",
			"Список инструментов: имя, группа, меняет ли что-то, описание"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := cmdPrinter(cmd)
			if group != "" && !knownGroup(group) {
				return clerrors.New(clerrors.CodeUsage, i18n.Tf("unknown group %q", "неизвестная группа %q", group),
					i18n.T("Groups: ", "Группы: ")+strings.Join(tools.Groups(), ", "))
			}
			var list []toolInfo
			for _, t := range newRegistry().Tools() {
				if group != "" && t.Group != group {
					continue
				}
				list = append(list, toolInfo{Name: t.Name, Group: t.Group, Mutating: t.Mutating, Description: t.Description,
					line: toolSummary(t)})
			}
			// Registration order scatters a group over the list; by group,
			// then by name, a group reads as one block.
			sort.Slice(list, func(i, j int) bool {
				if list[i].Group != list[j].Group {
					return list[i].Group < list[j].Group
				}
				return list[i].Name < list[j].Name
			})
			if p.IsJSON() {
				return p.JSON(list)
			}
			rows := make([][]string, len(list))
			for i, t := range list {
				rows[i] = []string{t.Name, t.Group, yesNo(t.Mutating), t.line}
			}
			p.Table([]string{i18n.T("NAME", "ИМЯ"), i18n.T("GROUP", "ГРУППА"), i18n.T("MUTATING", "МЕНЯЕТ"),
				i18n.T("DESCRIPTION", "ОПИСАНИЕ")}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&group, "group", "", i18n.T("Show only this group (", "Только эта группа (")+strings.Join(tools.Groups(), ", ")+")")
	_ = cmd.RegisterFlagCompletionFunc("group", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return tools.Groups(), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func knownGroup(g string) bool {
	for _, k := range tools.Groups() {
		if k == g {
			return true
		}
	}
	return false
}

// abbreviations end with a dot that does not end the sentence.
var abbreviations = []string{"e.g", "i.e", "incl", "etc"}

// endsWithAbbreviation reports whether s ends with one of abbreviations as
// a whole word ("incl", not "…pincl").
func endsWithAbbreviation(s string) bool {
	for _, a := range abbreviations {
		if rest, ok := strings.CutSuffix(s, a); ok && (rest == "" || strings.ContainsAny(rest[len(rest)-1:], " ([")) {
			return true
		}
	}
	return false
}

// summary returns the first sentence of s, cut to max runes.
func summary(s string, max int) string {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '.' && s[i+1] == ' ' && !endsWithAbbreviation(s[:i]) {
			s = s[:i+1]
			break
		}
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

// toolDescription is the tool's description in the language of the CLI:
// the English one (as MCP sends it), or the Russian summary and the rest of
// the Russian description. --json keeps the English one: it is the tool's
// definition, as the agent gets it.
func toolDescription(t tools.Tool) string {
	if i18n.Lang() == i18n.RU && t.ShortRU != "" && t.DescriptionRU != "" {
		return t.ShortRU + ".\n\n" + t.DescriptionRU
	}
	return t.Description
}

// toolSummary is the line of a tool in 'tools list': the first sentence of
// its description in the language of the CLI.
func toolSummary(t tools.Tool) string {
	if i18n.Lang() == i18n.RU && t.ShortRU != "" {
		return summary(t.ShortRU, 80)
	}
	return summary(t.Description, 80)
}

// --- tools describe ---

func newToolsDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "describe <name>",
		Short:             i18n.T("Show a tool's description and arguments", "Описание и аргументы инструмента"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeToolNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := lookupTool(newRegistry(), args[0])
			if err != nil {
				return err
			}
			p := cmdPrinter(cmd)
			if p.IsJSON() {
				return p.JSON(toolInfo{Name: t.Name, Group: t.Group, Mutating: t.Mutating,
					Description: t.Description, InputSchema: t.InputSchema})
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, i18n.T("%s (group %s", "%s (группа %s"), t.Name, t.Group)
			if t.Mutating {
				fmt.Fprint(w, i18n.T(", mutating: asks for confirmation", ", меняет ресурсы: спрашивает подтверждение"))
			} else if tools.IsSensitive(t) {
				fmt.Fprint(w, i18n.T(", reveals a secret: asks for confirmation", ", показывает секрет: спрашивает подтверждение"))
			}
			fmt.Fprintf(w, ")\n\n%s\n\n", toolDescription(t))
			params := schemaParams(t)
			if len(params) == 0 {
				fmt.Fprintln(w, i18n.T("No arguments.", "Аргументов нет."))
				return nil
			}
			fmt.Fprintln(w, i18n.T("Arguments (--arg name=value):", "Аргументы (--arg имя=значение):"))
			rows := make([][]string, len(params))
			for i, a := range params {
				req := ""
				if a.Required {
					req = i18n.T("required", "обязателен")
				}
				desc, _ := flagDesc(t, a)
				rows[i] = []string{a.Name, a.Type, req, desc}
			}
			p.Table([]string{i18n.T("NAME", "ИМЯ"), i18n.T("TYPE", "ТИП"), i18n.T("REQUIRED", "ОБЯЗАТЕЛЬНЫЙ"),
				i18n.T("DESCRIPTION", "ОПИСАНИЕ")}, rows)
			return nil
		},
	}
}

// param is one argument of a tool, from its JSON schema.
type param struct {
	Name        string
	Type        string // string, integer, number, boolean, array, object
	ItemType    string // for arrays
	Required    bool
	Description string
	Default     any // the schema's "default", when it declares one
}

// schemaParams lists the tool's arguments, required first, then by name.
// confirmed is left out: consent is given with --yes or at the prompt.
func schemaParams(t tools.Tool) []param {
	props, _ := t.InputSchema["properties"].(map[string]any)
	required := map[string]bool{}
	switch r := t.InputSchema["required"].(type) {
	case []string:
		for _, n := range r {
			required[n] = true
		}
	case []any:
		for _, n := range r {
			if s, ok := n.(string); ok {
				required[s] = true
			}
		}
	}
	var out []param
	for name, v := range props {
		if name == "confirmed" {
			continue
		}
		m, _ := v.(map[string]any)
		a := param{Name: name, Required: required[name]}
		a.Type, _ = m["type"].(string)
		a.Description, _ = m["description"].(string)
		a.Default = m["default"]
		if items, ok := m["items"].(map[string]any); ok {
			a.ItemType, _ = items["type"].(string)
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func lookupTool(reg *tools.Registry, name string) (tools.Tool, error) {
	t, ok := reg.Lookup(name)
	if !ok {
		return tools.Tool{}, clerrors.New(clerrors.CodeUsage, i18n.Tf("unknown tool %q", "неизвестный инструмент %q", name),
			i18n.T("Run 'evo tools list' to see the tools.", "Список инструментов: 'evo tools list'."))
	}
	return t, nil
}

func completeToolNames(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, t := range newRegistry().Tools() {
		names = append(names, t.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// --- tools call ---

func newToolsCallCmd() *cobra.Command {
	var (
		argPairs []string
		argsJSON string
		argsFile string
		wait     bool
	)
	cmd := &cobra.Command{
		Use:               "call <name>",
		Short:             i18n.T("Call a tool and print its JSON result", "Вызвать инструмент и напечатать его ответ (JSON)"),
		Long:              i18n.T(toolsCallHelpEN, toolsCallHelpRU),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeToolNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToolsCall(cmd, args[0], argsFile, argsJSON, argPairs, wait)
		},
	}
	cmd.Flags().StringArrayVar(&argPairs, "arg", nil, i18n.T("Tool argument as name=value (repeatable)", "Аргумент инструмента: имя=значение (флаг повторяется)"))
	cmd.Flags().StringVar(&argsJSON, "args-json", "", i18n.T("Tool arguments as a JSON object", "Аргументы инструмента объектом JSON"))
	cmd.Flags().StringVar(&argsFile, "args-file", "", i18n.T("Read tool arguments from a JSON file", "Прочитать аргументы инструмента из файла JSON"))
	cmd.Flags().BoolVar(&wait, "wait", false, i18n.T("For tools that take wait_seconds (*_operation, stack_status): wait for the final state, polling with backoff up to the tool's limit",
		"Для инструментов с wait_seconds (*_operation, stack_status): ждать конечного состояния, опрашивая с нарастающей паузой до предела инструмента"))
	return cmd
}

const toolsCallHelpRU = `Вызвать инструмент и напечатать его ответ (JSON) как есть.

Аргументы берутся из --args-file, затем из --args-json, затем из --arg
(более поздние перекрывают ранние). Значения --arg приводятся к типам схемы
инструмента: integer, number и boolean разбираются; array — список через
запятую (или массив JSON); object — JSON. Имена и типы из JSON
сверяются со схемой так же, как в MCP (массив можно передать и строкой
через запятую); null означает «не задан».

  evo tools call list_zones
  evo tools call vm_get --arg name=web
  evo tools call open_ports --arg security_group=web-sg --arg ports=80,443 --yes
  evo tools call pg_operation --arg operation_id=OP --wait

Коды выхода: 0 — успех; 3 — нет подтверждения; ошибка инструмента с кодом
(API_UNREACHABLE, AUTH_FAILED, …) — код выхода этого кода, как у других
команд (см. docs/exit-codes.md); без кода — 4, если инструмент сообщил, что
объект не найден, иначе 5.`

const toolsCallHelpEN = `Call a tool and print its JSON result as is.

Arguments come from --args-file, then --args-json, then --arg (later ones
win). --arg values are converted by the tool's schema: integer, number and
boolean are parsed; an array is a comma-separated list (or a JSON array);
an object is JSON. Names and types from the JSON sources are checked
against the schema as MCP checks them (an array may also be a
comma-separated string); null means "not given".

  evo tools call list_zones
  evo tools call vm_get --arg name=web
  evo tools call open_ports --arg security_group=web-sg --arg ports=80,443 --yes
  evo tools call pg_operation --arg operation_id=OP --wait

Exit codes: 0 success; 3 confirmation not given; an error the tool reports
with a code (API_UNREACHABLE, AUTH_FAILED, ...) exits as that code does for
any other command (see docs/exit-codes.md); without a code, 4 when the tool
reported that something was not found, 5 otherwise.`

func runToolsCall(cmd *cobra.Command, name, argsFile, argsJSON string, argPairs []string, wait bool) error {
	t, err := lookupTool(newRegistry(), name)
	if err != nil {
		return err
	}
	callArgs, err := buildToolArgs(t, argsFile, argsJSON, argPairs)
	if err != nil {
		return err
	}
	if wait {
		if err := applyWait(t, callArgs); err != nil {
			return err
		}
	}
	text, err := invokeTool(cmd.Context(), cmd, t, callArgs, false)
	// With --json a tool error goes to stderr only, as {"error":…}, and
	// stdout stays empty, as in the command trees: pg_get without an id
	// printed its error body to stdout too, ar_get did not (QA-12).
	if text != "" && (err == nil || !flagJSON) {
		fmt.Fprintln(cmd.OutOrStdout(), text)
	}
	return err
}

// buildToolArgs merges the argument sources and checks them against the
// tool's schema.
func buildToolArgs(t tools.Tool, file, rawJSON string, pairs []string) (map[string]any, error) {
	args := map[string]any{}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, clerrors.Wrap(clerrors.CodeUsage, i18n.T("read --args-file: ", "чтение --args-file: ")+err.Error(), "", err)
		}
		if err := mergeJSONArgs(args, data, "--args-file"); err != nil {
			return nil, err
		}
	}
	if rawJSON != "" {
		if err := mergeJSONArgs(args, []byte(rawJSON), "--args-json"); err != nil {
			return nil, err
		}
	}
	params := map[string]param{}
	for _, p := range schemaParams(t) {
		params[p.Name] = p
	}
	// A misspelt name in JSON is refused as one of --arg is; its types are
	// checked below, by the tool's own check.
	for name, v := range args {
		if name == "confirmed" {
			continue // refused below with a hint
		}
		if v == nil {
			delete(args, name) // null: as if not given
			continue
		}
		if _, known := params[name]; !known {
			return nil, unknownArgError(t, name)
		}
	}
	for _, pair := range pairs {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || name == "" {
			return nil, clerrors.New(clerrors.CodeUsage, i18n.Tf("--arg %q: want name=value", "--arg %q: нужно имя=значение", pair), "")
		}
		p, known := params[name]
		if !known {
			return nil, unknownArgError(t, name)
		}
		v, err := coerceArg(p, value)
		if err != nil {
			return nil, clerrors.New(clerrors.CodeUsage, fmt.Sprintf("--arg %s: %v", name, err), "")
		}
		args[name] = v
	}
	if _, ok := args["confirmed"]; ok {
		return nil, clerrors.New(clerrors.CodeUsage, i18n.T("confirmed is not an argument here", "confirmed здесь не аргумент"),
			i18n.T("Confirm with --yes or at the prompt; --dry-run shows the preview only.",
				"Подтвердите флагом --yes или ответом на вопрос; --dry-run покажет только превью."))
	}
	// The types of the arguments and the enum values: the check every call
	// of the tool makes, MCP included (tools.CheckArgs, spec.md REQ-8) — a
	// string where the schema wants a number would otherwise reach the
	// handler, which silently takes its default; a string for an array is
	// the form MCP takes ("80,443").
	if err := tools.CheckArgs(t, args); err != nil {
		return nil, err
	}
	for name, v := range args {
		if items, ok := v.([]any); ok {
			if err := checkItems(params[name], items); err != nil {
				return nil, clerrors.New(clerrors.CodeUsage, i18n.Tf("argument %s: %v", "аргумент %s: %v", name, err), describeHint(t.Name))
			}
		}
	}
	var missing []string
	for _, p := range schemaParams(t) {
		// An empty string is no value: the handler would answer
		// "required" as a tool error (exit 5) instead of a usage error.
		if v, ok := args[p.Name]; p.Required && (!ok || v == "") {
			missing = append(missing, p.Name)
		}
	}
	if len(missing) > 0 {
		return nil, clerrors.New(clerrors.CodeUsage,
			i18n.Tf("%s: missing required argument(s): %s", "%s: не заданы обязательные аргументы: %s", t.Name, strings.Join(missing, ", ")),
			describeHint(t.Name))
	}
	return args, nil
}

func describeHint(tool string) string {
	return i18n.Tf("Run 'evo tools describe %s'.", "Аргументы инструмента: 'evo tools describe %s'.", tool)
}

func mergeJSONArgs(dst map[string]any, data []byte, from string) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return clerrors.New(clerrors.CodeUsage, i18n.Tf("%s: not a JSON object: %v", "%s: не объект JSON: %v", from, err), "")
	}
	for k, v := range m {
		dst[k] = v
	}
	return nil
}

func unknownArgError(t tools.Tool, name string) error {
	var names []string
	for _, p := range schemaParams(t) {
		names = append(names, p.Name)
	}
	takes := i18n.T("no arguments", "нет аргументов")
	if len(names) > 0 {
		takes = strings.Join(names, ", ")
	}
	return clerrors.New(clerrors.CodeUsage, i18n.Tf("%s has no argument %q", "у %s нет аргумента %q", t.Name, name),
		i18n.Tf("%s takes: %s. See 'evo tools describe %s'.", "Аргументы %s: %s. Подробнее: 'evo tools describe %s'.", t.Name, takes, t.Name))
}

// coerceArg converts a --arg value to the type the schema declares, in the
// form a JSON decoder would produce (numbers as float64, arrays as []any).
func coerceArg(p param, value string) (any, error) {
	switch p.Type {
	case "integer":
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("%q is not an integer", "%q — не целое число"), value)
		}
		return float64(n), nil
	case "number":
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		// NaN and ±Inf parse, but JSON cannot carry them.
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf(i18n.T("%q is not a number", "%q — не число"), value)
		}
		return f, nil
	case "boolean":
		b, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf(i18n.T("%q is not true or false", "%q — не true и не false"), value)
		}
		return b, nil
	case "array":
		if strings.HasPrefix(strings.TrimSpace(value), "[") {
			var a []any
			if err := json.Unmarshal([]byte(value), &a); err != nil {
				return nil, fmt.Errorf(i18n.T("not a JSON array: %v", "не массив JSON: %v"), err)
			}
			if err := checkItems(p, a); err != nil {
				return nil, err
			}
			return a, nil
		}
		if p.ItemType == "object" || p.ItemType == "array" {
			// JSON items contain commas themselves: one value, one item.
			v, err := coerceArg(param{Type: p.ItemType}, value)
			if err != nil {
				return nil, err
			}
			return []any{v}, nil
		}
		out := []any{}
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			v, err := coerceArg(param{Type: p.ItemType}, part)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case "object":
		var m map[string]any
		if err := json.Unmarshal([]byte(value), &m); err != nil {
			return nil, fmt.Errorf(i18n.T("not a JSON object: %v", "не объект JSON: %v"), err)
		}
		return m, nil
	}
	return value, nil
}

// checkItems checks the items of a JSON array against the item type of p
// by the rule of the tool's own check for a scalar (tools.CheckArgs):
// ports=[80,"x"] is refused as ports=80,x is, and so is ports [80,"x"] in
// --args-json, as the CLI always did. It is the one check the CLI makes
// beyond the tool's: MCP leaves the items to the handler (docs/mcp.md:
// "arrays are not checked by type"), which refuses a port "x" but drops a
// domain 1 of cert_issue_le dns_names silently. Checking the items in
// tools.CheckArgs would refuse over MCP what MCP takes in 1.1 — a question
// to the owner (report of PR 6, spec.md REQ-8); until then the CLI keeps
// its check, and nothing the CLI took is refused.
func checkItems(p param, items []any) error {
	if p.ItemType == "" {
		return nil
	}
	for i, v := range items {
		name := fmt.Sprintf("%s[%d]", p.Name, i)
		// The tool's check takes any array or object (above); an item of
		// an array of objects is an object, as the CLI always required.
		_, isObject := v.(map[string]any)
		_, isArray := v.([]any)
		if p.ItemType == "object" && !isObject || p.ItemType == "array" && !isArray {
			b, _ := json.Marshal(v)
			return fmt.Errorf(i18n.T("%s must be a JSON %s, got %s", "%s должен быть JSON %s, получено: %s"), name, p.ItemType, summary(string(b), 40))
		}
		item := tools.Tool{Name: p.Name, InputSchema: map[string]any{"type": "object",
			"properties": map[string]any{name: map[string]any{"type": p.ItemType}}}}
		if err := tools.CheckArgs(item, map[string]any{name: v}); err != nil {
			var ce *clerrors.CLIError
			if stderrors.As(err, &ce) {
				return stderrors.New(ce.Message)
			}
			return err
		}
	}
	return nil
}

// applyWait sets wait_seconds to the longest wait the tool allows, unless
// the caller gave one. The tool polls with capped backoff itself.
func applyWait(t tools.Tool, args map[string]any) error {
	props, _ := t.InputSchema["properties"].(map[string]any)
	if _, ok := props["wait_seconds"]; !ok {
		return clerrors.New(clerrors.CodeUsage, i18n.Tf("%s does not support --wait", "%s не поддерживает --wait", t.Name),
			i18n.T("--wait works with tools that take wait_seconds: the *_operation tools and stack_status.",
				"--wait работает с инструментами, у которых есть wait_seconds: *_operation и stack_status."))
	}
	if _, ok := args["wait_seconds"]; !ok {
		args["wait_seconds"] = float64(tools.OpWaitMaxSeconds)
	}
	return nil
}

// invokeTool calls the tool. When the tool answers confirmation_required
// (or is mutating but has no gate of its own), consent goes through
// confirm.Ask and, once given, the tool is called again with
// confirmed=true. It returns the tool's final JSON text ("" when the call
// stopped at a --dry-run preview) and an error carrying the exit code.
// human (the command trees) shows the structured preview of a billable
// tool in the words of deploy; 'evo tools call' shows the answer as is.
func invokeTool(ctx context.Context, cmd *cobra.Command, t tools.Tool, args map[string]any, human bool) (string, error) {
	opts := confirm.Options{Yes: flagYes, DryRun: flagDryRun, JSON: flagJSON,
		Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}

	if t.Mutating && !takesConfirmed(t) {
		ok, err := confirm.Ask(ctx, confirm.Preview{Action: t.Name, Summary: callSummary(t.Name),
			Lines: argLines(args), Details: args}, opts)
		if !ok {
			return "", err
		}
	}

	text, isErr := t.Handler(ctx, args)
	if resp := parseObject(text); resp["status"] == "confirmation_required" {
		preview := toolPreview(t.Name, resp)
		if human {
			if p, ok := createPreview(t.Name, resp); ok {
				preview = p
			}
		}
		ok, err := confirm.Ask(ctx, preview, opts)
		if !ok {
			return "", err
		}
		confirmed := make(map[string]any, len(args)+1)
		for k, v := range args {
			confirmed[k] = v
		}
		confirmed["confirmed"] = true
		text, isErr = t.Handler(ctx, confirmed)
	}
	if isErr {
		return text, toolError(t.Name, text)
	}
	return text, nil
}

func takesConfirmed(t tools.Tool) bool {
	props, _ := t.InputSchema["properties"].(map[string]any)
	_, ok := props["confirmed"]
	return ok
}

func parseObject(text string) map[string]any {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return nil
	}
	return m
}

// toolPreview turns a confirmation_required answer into a confirm.Preview:
// the message first, then every other field.
func toolPreview(name string, resp map[string]any) confirm.Preview {
	p := confirm.Preview{Action: name, Summary: callSummary(name), Details: resp}
	if msg, ok := resp["message"].(string); ok && msg != "" {
		p.Lines = append(p.Lines, msg)
	}
	rest := map[string]any{}
	for k, v := range resp {
		if k != "status" && k != "message" {
			rest[k] = v
		}
	}
	p.Lines = append(p.Lines, argLines(rest)...)
	return p
}

// createPreview is the human preview of a billable tool: the "preview"
// object of its answer, shown in the words of deploy. The tool has
// completed it (zone names, the flavor, the image, the price: spec REQ-7);
// the CLI only shows it, so the command, 'evo tools call' and MCP give one
// preview. --json gets the same answer with the object as the tool gave it.
func createPreview(name string, resp map[string]any) (confirm.Preview, bool) {
	raw, ok := resp["preview"]
	if !ok {
		return confirm.Preview{}, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return confirm.Preview{}, false
	}
	var cp engine.CreatePreview
	if json.Unmarshal(b, &cp) != nil || cp.Resource == "" {
		return confirm.Preview{}, false
	}
	p := engine.ShowCreatePreview(name, cp)
	details := make(map[string]any, len(resp))
	for k, v := range resp {
		details[k] = v
	}
	// The object as the tool's struct orders it, as before (the JSON of the
	// command keeps its key order).
	details["preview"] = cp
	p.Details = details
	return p, true
}

func callSummary(tool string) string { return i18n.T("call ", "вызвать ") + tool }

// argLines renders a map as "key: value" lines in key order.
func argLines(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		var v string
		if s, ok := m[k].(string); ok {
			v = s
		} else {
			b, _ := json.Marshal(m[k])
			v = string(b)
		}
		lines = append(lines, k+": "+v)
	}
	return lines
}

// toolError maps a tool's error answer to a CLIError, as the same failure
// of any other command would be: the code ("reason" of the answer, else the
// "API_UNREACHABLE: …" prefix of its message) keeps its exit code — 1 for
// API_UNREACHABLE, 2 for credentials, 4 for not found — and the hint of the
// answer is kept. An answer without a code gives 4 when it says something
// was not found, and API_ERROR (5) otherwise.
func toolError(name, text string) error {
	msg := text
	var reason, hint string
	if m := parseObject(text); m != nil {
		for _, k := range []string{"error", "message"} {
			if s, ok := m[k].(string); ok && s != "" {
				msg = s
				break
			}
		}
		reason, _ = m["reason"].(string)
		hint, _ = m["hint"].(string)
	}
	code := ""
	if clerrors.IsCode(reason) {
		code = reason
	}
	if prefix, rest, ok := strings.Cut(msg, ": "); ok && clerrors.IsCode(prefix) && (code == "" || code == prefix) {
		code, msg = prefix, rest
	}
	if code == "" {
		code = clerrors.CodeAPIError
		if low := strings.ToLower(text); strings.Contains(low, "not found") || strings.Contains(low, "не найден") {
			code = clerrors.CodeNotFound
		}
	}
	if hint == "" {
		switch {
		case code == clerrors.CodeUsage: // the tool refused its arguments; nothing reached the cloud
			hint = i18n.T("Check the arguments: 'evo tools describe ", "Проверьте аргументы: 'evo tools describe ") + name + "'."
		case code == clerrors.CodeConfigInvalid: // the message names what to set
		case clerrors.ExitCode(clerrors.New(code, "", "")) == clerrors.ExitAuth:
			hint = i18n.T("Run 'evo configure', or set CLOUDRU_KEY_ID and CLOUDRU_SECRET.",
				"Выполните 'evo configure' или задайте CLOUDRU_KEY_ID и CLOUDRU_SECRET.")
		}
	}
	return clerrors.New(code, name+": "+msg, hint)
}

// Root flags that pick the project and the zone for every command that
// talks to the cloud. A command's own flag of the same name (deploy,
// configure) takes precedence; so does an argument a tool passes.
var (
	flagProjectID string
	flagRegion    string
)

func addScopeFlags(root *cobra.Command) {
	root.PersistentFlags().StringVar(&flagProjectID, "project-id", "", i18n.T("Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)",
		"UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)"))
	root.PersistentFlags().StringVar(&flagRegion, "region", "", i18n.T("Availability zone UUID (default: from the profile or $CLOUDRU_REGION)",
		"UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)"))
}

// applyScopeFlags adds --project-id and --region to the engine overrides
// unless the caller already set them.
func applyScopeFlags(overrides map[string]any) {
	if _, ok := overrides["ProjectID"]; !ok && flagProjectID != "" {
		overrides["ProjectID"] = flagProjectID
	}
	if _, ok := overrides["Region"]; !ok && flagRegion != "" {
		overrides["Region"] = flagRegion
	}
}
