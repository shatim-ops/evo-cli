// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The command trees (vm, image, disk, vpc, k8s, pg, s3, dns, lb and the
// rest) are built from the registry: each tool's CLISpec (internal/tools) names its
// command, which of its arguments are positional, and how the answer is
// shown. Every other argument of the tool becomes a flag with the type from
// its schema. The cobra layer only parses flags into map[string]any, calls
// the tool and formats the answer. The help of a command is the tool's
// description, English or Russian (Tool.ShortRU, DescriptionRU,
// ArgDescriptionsRU). The presentation stays here: the summary of each tree
// (in Russian: trees_ru.go), the Russian tables and the views a CLISpec
// names by Render.

// treeGroup is one top-level command.
type treeGroup struct {
	Name  string
	Short string
	Cmds  []treeCmd
}

// treeCmd is one command of a tree: the tool's CLISpec, and what the CLI
// adds to it.
type treeCmd struct {
	tools.CLISpec
	Tool string
	// russian: the human table of a Russian tree (vm, disk, dns, …)
	// speaks Russian — column names, да/нет, the note under a list. Set
	// from russianTrees; --json keeps the tool's answer.
	russian bool
	// headers are the Russian column titles the tree has beyond
	// columnHeaderRU (treeHeadersRU).
	headers map[string]string
	// render is the view CLISpec.Render names (treeRenders).
	render renderFunc
}

// renderFunc shows a tool's answer instead of the generic table.
type renderFunc func(p *output.Printer, w io.Writer, text string) error

// treeRenders is the closed set of views a CLISpec.Render may name (image
// list, image get, the Marketplace showcase, k8s addons, ca logs, metrics
// scrape); --json still prints the tool's answer as is.
var treeRenders = map[string]renderFunc{
	"image-list":       renderImageList,
	"image-card":       renderImageCard,
	"marketplace-list": renderMarketplaceList,
	"addon-list":       renderAddonList,
	"addon-versions":   renderAddonVersions,
	"log-lines":        renderLogLines,
	"exposition":       renderExposition,
	"project-list":     renderProjectList,
}

// treeGroupShort is the English summary of each tree (treeGroupShortRU is
// the Russian one). A CLISpec group without one fails TestTrees_BuildAndHelp.
var treeGroupShort = map[string]string{
	"vm":          "Virtual machines",
	"image":       "VM images: operating systems and Marketplace products",
	"marketplace": "Showcase: VM images and Kubernetes addons, with the command that deploys each",
	"disk":        "Disks",
	"vpc":         "Networks: VPCs, routes, subnets, security groups, floating IPs",
	"k8s":         "Managed Kubernetes clusters",
	"pg":          "Managed PostgreSQL clusters, databases, users, backups",
	"s3":          "Object Storage: buckets and objects",
	"secret":      "Secret Manager",
	"dns":         "Evolution DNS: zones and records",
	"cert":        "Certificate Manager: TLS certificates",
	"lb":          "Network load balancers (L4)",
	"tg":          "Target groups of the load balancers",
	"snat":        "SNAT gateways: outbound internet for private subnets",
	"kafka":       "Managed Kafka clusters",
	"redis":       "Managed Redis clusters",
	"ar":          "Artifact Registry: registries of images and packages",
	"ca":          "Container Apps: serverless containers",
	"billing":     "Billing: balance, consumption, FinOps report",
	"audit":       "Audit Logging: who did what in the project",
	"org":         "Organization: projects and quotas",
	"iam":         "IAM: service accounts",
	"metrics":     "Service Monitoring: metrics",
	"stack":       "Stacks: several resources from one manifest",
}

// treeGroupsOf groups the commands of reg's tools by CLISpec.Group, sorted
// by group and verb (cobra lists them so anyway). Tools without a command
// (CLIPending, None) are left out, and so are the engine commands, which
// internal/cli builds by hand (engineCommands). A group "" holds the
// commands at the root.
func treeGroupsOf(reg *tools.Registry) []treeGroup {
	byGroup := map[string][]treeCmd{}
	for _, t := range reg.Tools() {
		if t.CLI.Verb == "" {
			continue
		}
		if _, ok := engineCommandOf(t.Name); ok {
			continue // built by hand (engine_commands.go)
		}
		c := treeCmd{CLISpec: t.CLI, Tool: t.Name, russian: russianTrees[t.CLI.Group], headers: treeHeadersRU(t.CLI.Group)}
		if t.CLI.Render != "" {
			c.render = treeRenders[t.CLI.Render]
			if c.render == nil {
				panic("command tree: " + t.Name + ": no view " + t.CLI.Render) // caught by TestParity_CommandsAndTools
			}
		}
		byGroup[t.CLI.Group] = append(byGroup[t.CLI.Group], c)
	}
	out := make([]treeGroup, 0, len(byGroup))
	for name, cmds := range byGroup {
		sort.Slice(cmds, func(i, j int) bool { return cmds[i].Verb < cmds[j].Verb })
		g := treeGroup{Name: name, Cmds: cmds}
		switch {
		case name == "":
		case engineToolAt("evo "+name) != "":
			// The commands of an engine command (evo deploy plan): its
			// help is its own.
		default:
			short, ok := treeGroupShort[name]
			if !ok {
				panic("command tree: no summary for group " + name) // caught by TestTrees_BuildAndHelp
			}
			g.Short = short
			if ru, ok := treeGroupShortRU[name]; ok {
				g.Short = i18n.T(short, ru)
			}
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// toolAnnotation marks a command built from a tool's CLISpec with the
// tool's name (TestParity_CommandsAndTools reads it).
const toolAnnotation = "evo.tool"

// rootArgs are tool arguments served by root flags (--project-id,
// --region) through the engine configuration, and confirmed, which is
// given with --yes or at the prompt.
var rootArgs = map[string]bool{"project_id": true, "region": true, "confirmed": true}

// newTreeCmds builds the command trees from the registry's CLISpecs. A
// group that is an engine command of root (evo deploy) gets the commands
// of its tools as subcommands, beside its own action (spec.md R7): no
// second root command of that name.
func newTreeCmds(root *cobra.Command) []*cobra.Command {
	reg := newRegistry()
	var out []*cobra.Command
	for _, g := range treeGroupsOf(reg) {
		var parent *cobra.Command
		if g.Name != "" {
			if engine := engineCommandIn(root, g.Name); engine != nil {
				parent = engine
			} else {
				parent = asGroup(&cobra.Command{Use: g.Name, Short: g.Short})
				out = append(out, parent)
			}
		}
		for _, c := range g.Cmds {
			t, _ := reg.Lookup(c.Tool)
			leaf := newTreeLeaf(c, t)
			if parent == nil {
				out = append(out, leaf)
				continue
			}
			parent.AddCommand(leaf)
		}
	}
	return out
}

// engineCommandIn is the engine command of root named name, nil if none.
func engineCommandIn(root *cobra.Command, name string) *cobra.Command {
	if root == nil {
		return nil
	}
	for _, c := range root.Commands() {
		if c.Name() == name && c.Annotations[toolAnnotation] != "" {
			if _, ok := engineCommandOf(c.Annotations[toolAnnotation]); ok {
				return c
			}
		}
	}
	return nil
}

// positional is one parsed entry of treeCmd.Args.
type positional struct {
	names    []string // one name, or two for "id|name"
	optional bool
	rest     bool
}

func parsePositionals(specs []string) []positional {
	out := make([]positional, 0, len(specs))
	for _, s := range specs {
		p := positional{}
		switch {
		case strings.HasSuffix(s, "..."):
			p.rest, s = true, strings.TrimSuffix(s, "...")
		case strings.HasSuffix(s, "?"):
			p.optional, s = true, strings.TrimSuffix(s, "?")
		}
		p.names = strings.Split(s, "|")
		out = append(out, p)
	}
	return out
}

// flagName is the flag for a tool argument: snake_case → kebab-case.
func (c treeCmd) flagName(arg string) string {
	if n, ok := c.Rename[arg]; ok {
		return n
	}
	return strings.ReplaceAll(arg, "_", "-")
}

// flagArgs are the tool arguments that become flags: everything in the
// schema that is not positional and not served by the root.
func (c treeCmd) flagArgs(t tools.Tool) []param {
	pos := map[string]bool{}
	for _, p := range parsePositionals(c.Args) {
		for _, n := range p.names {
			pos[n] = true
		}
	}
	var out []param
	for _, p := range schemaParams(t) {
		if pos[p.Name] || c.Omit[p.Name] || (rootArgs[p.Name] && c.Rename[p.Name] == "") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func newTreeLeaf(c treeCmd, t tools.Tool) *cobra.Command {
	pos := parsePositionals(c.Args)
	use := []string{c.Verb}
	minArgs, maxArgs := 0, 0
	for _, p := range pos {
		label := strings.ReplaceAll(strings.Join(p.names, "|"), "_", "-")
		switch {
		case p.rest:
			use = append(use, "<"+label+">...")
			maxArgs = -1
		case p.optional:
			use = append(use, "["+label+"]")
			maxArgs++
		default:
			use = append(use, "<"+label+">")
			minArgs++
			maxArgs++
		}
	}
	if len(pos) > 0 && pos[len(pos)-1].rest {
		minArgs++
	}

	short := summary(t.Description, 100)
	long := t.Description + "\n\nRuns the " + t.Name + " tool (see 'evo tools describe " + t.Name + "')."
	if t.Mutating {
		long += "\nAsks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only."
	} else if tools.IsSensitive(t) {
		long += "\nReveals a secret, so it asks for confirmation: a prompt on a terminal, --yes to confirm in advance."
	}
	if i18n.Lang() == i18n.RU {
		if t.ShortRU != "" {
			short = t.ShortRU
		}
		runs := "Вызывает инструмент " + t.Name + " (подробнее: 'evo tools describe " + t.Name + "')."
		if t.DescriptionRU != "" {
			long = short + ".\n\n" + t.DescriptionRU + "\n\n" + runs
		} else {
			long = short + ".\n\n" + runs + "\n" +
				"Описание инструмента и его флагов — на английском, как в MCP:\n\n" + t.Description
		}
		if t.Mutating {
			long += "\n\nСпрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью."
		} else if tools.IsSensitive(t) {
			long += "\n\nПоказывает секрет, поэтому спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее."
		}
	}

	cmd := &cobra.Command{
		Use:         strings.Join(use, " "),
		Short:       short,
		Long:        long,
		Annotations: map[string]string{toolAnnotation: t.Name},
		Args: func(cmd *cobra.Command, args []string) error {
			if maxArgs == 0 && len(args) > 0 {
				return fmt.Errorf(i18n.T("%s takes no arguments, got %q", "%s не принимает аргументов, передано %q"), cmd.CommandPath(), args[0])
			}
			if len(args) < minArgs || (maxArgs >= 0 && len(args) > maxArgs) {
				return fmt.Errorf(i18n.T("%s: want %s, got %d argument(s)", "%s: нужно %s, передано аргументов: %d"),
					cmd.CommandPath(), strings.Join(use[1:], " "), len(args))
			}
			return nil
		},
	}

	if maxArgs < 0 {
		// Everything after the fixed arguments belongs to the tool
		// (kubectl get pods -n x): stop parsing flags at the first argument.
		// evo's own flags go before it.
		cmd.Flags().SetInterspersed(false)
		long += i18n.T("\nevo flags (--json, --yes, --dry-run, --profile) go before the arguments: everything after the first one is passed to the tool as is.",
			"\nФлаги evo (--json, --yes, --dry-run, --profile) ставятся перед аргументами: всё после первого аргумента передаётся инструменту как есть.")
		cmd.Long = long
	}

	flagArgs := c.flagArgs(t)
	for _, p := range flagArgs {
		name := c.flagName(p.Name)
		desc, ru := flagDesc(t, p)
		// The notes follow the language of the description they extend.
		note := func(en, ruNote string) string {
			if ru {
				return ruNote
			}
			return en
		}
		if c.Pairs[p.Name] {
			cmd.Flags().StringArray(name, nil, desc+note(" (key=value, repeatable; the value may contain '='. "+
				"key=- reads the value from stdin (one field at most), key=@file from a file, its trailing newline trimmed: "+
				"use these for passwords, so the value stays out of the command line and the shell history)",
				" (поле=значение, флаг повторяется; значение может содержать '='. "+
					"поле=- читает значение из stdin (не больше одного поля), поле=@файл — из файла без завершающего перевода строки: "+
					"так пароль не попадает в командную строку и историю оболочки)"))
			continue
		}
		if c.Files[p.Name] {
			cmd.Flags().String(name, "", note("A file with the value ('-' reads stdin until EOF: Ctrl-D on a terminal). ", "Файл со значением ('-' — из stdin до EOF: на терминале — Ctrl-D). ")+desc)
			continue
		}
		switch p.Type {
		case "integer":
			cmd.Flags().Int(name, 0, desc)
		case "number":
			cmd.Flags().Float64(name, 0, desc)
		case "boolean":
			// An argument the tool takes as true when absent defaults to
			// true here too: the help says "(default true)" and
			// --x=false turns it off. An unset flag still goes to the
			// tool as nothing (treeArgs takes changed flags only).
			on, _ := p.Default.(bool)
			if on {
				desc = defaultTrueRE.ReplaceAllString(desc, "") + note(" To turn off: --"+name+"=false", " Выключить: --"+name+"=false")
			}
			cmd.Flags().Bool(name, on, desc)
		case "array":
			cmd.Flags().StringArray(name, nil, desc+note(" (repeatable, or comma-separated)", " (флаг повторяется или значения через запятую)"))
		default:
			cmd.Flags().String(name, "", desc)
		}
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		callArgs, err := treeArgs(cmd, c, t, pos, flagArgs, args)
		if err != nil {
			return err
		}
		text, err := invokeTool(cmd.Context(), cmd, t, callArgs, true)
		if flagJSON {
			// On a tool error the text is the tool's error body: the error
			// itself goes to stderr as {"error":…}, so stdout stays empty
			// for jq. The confirmation_required and dry_run envelopes are
			// written by confirm.Ask, not returned here.
			if text != "" && err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), text)
			}
			return err
		}
		if c.Raw && printRaw(cmd.OutOrStdout(), cmd.ErrOrStderr(), text, err) {
			return err
		}
		if err != nil || text == "" {
			return err
		}
		if c.render != nil {
			return c.render(cmdPrinter(cmd), cmd.OutOrStdout(), text)
		}
		return renderAnswer(cmdPrinter(cmd), cmd.OutOrStdout(), text, tableView{Columns: c.Columns, Russian: c.russian, Headers: c.headers})
	}
	return cmd
}

// defaultTrueRE is the "(default: true)" of a schema description: pflag
// adds its own "(default true)" to the help of a flag that defaults to true.
var defaultTrueRE = regexp.MustCompile(` ?\(default:? true\)`)

// flagDesc is the help of the flag for argument p: the tool's Russian text
// of the argument when the CLI speaks Russian and there is one (ru is then
// true), the schema's English description otherwise.
func flagDesc(t tools.Tool, p param) (desc string, ru bool) {
	if i18n.Lang() == i18n.RU {
		if s := t.ArgDescriptionsRU[p.Name]; s != "" {
			return s, true
		}
	}
	return p.Description, false
}

// printRaw prints the "output" of a kubectl, apply or helm answer as the
// tool printed it: to stdout, or to stderr when the tool failed (kubectl's
// own message, which the error alone does not carry). False when the answer
// has no output (a refusal before kubectl ran): it is shown as usual.
func printRaw(stdout, stderr io.Writer, text string, err error) bool {
	out, ok := parseObject(text)["output"].(string)
	if !ok {
		return false
	}
	w := stdout
	if err != nil {
		w = stderr
	}
	fmt.Fprint(w, out)
	if out != "" && !strings.HasSuffix(out, "\n") {
		fmt.Fprintln(w)
	}
	return true
}

// evoFlagsKubectlLacks are evo's global flags that kubectl does not have.
// After the first argument they would reach kubectl ("unknown flag"), so they
// are refused with the right order instead. --dry-run and --profile are
// kubectl flags too and pass through.
var evoFlagsKubectlLacks = map[string]bool{"--json": true, "--yes": true, "-y": true}

// evoFlagInRest refuses an evo flag placed in the pass-through tail, up to a
// "--" (what follows it is a command inside the container).
func evoFlagInRest(cmd *cobra.Command, rest []string, firstArg string) error {
	for _, a := range rest {
		if a == "--" {
			return nil
		}
		if evoFlagsKubectlLacks[a] || strings.HasPrefix(a, "--json=") || strings.HasPrefix(a, "--yes=") {
			return clerrors.New(clerrors.CodeUsage,
				i18n.Tf("%s: %s after the arguments would go to the tool", "%s: %s после аргументов достался бы инструменту", cmd.CommandPath(), a),
				i18n.Tf("Put evo flags first: '%s %s <%s> …'.", "Флаги evo ставьте первыми: '%s %s <%s> …'.",
					cmd.CommandPath(), strings.SplitN(a, "=", 2)[0], firstArg))
		}
	}
	return nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// treeArgs turns positionals and changed flags into the tool's arguments.
func treeArgs(cmd *cobra.Command, c treeCmd, t tools.Tool, pos []positional, flagArgs []param, args []string) (map[string]any, error) {
	out := map[string]any{}
	i := 0
	for _, p := range pos {
		if i >= len(args) {
			break
		}
		if args[i] == "" && !p.optional {
			label := strings.ReplaceAll(strings.Join(p.names, "|"), "_", "-")
			return nil, clerrors.New(clerrors.CodeUsage,
				i18n.Tf("%s: <%s> is empty", "%s: <%s> пустой", cmd.CommandPath(), label),
				helpHint(cmd))
		}
		switch {
		case p.rest:
			if err := evoFlagInRest(cmd, args[i:], strings.ReplaceAll(strings.Join(pos[0].names, "|"), "_", "-")); err != nil {
				return nil, err
			}
			out[p.names[0]] = strings.Join(args[i:], " ")
			i = len(args)
		case len(p.names) == 2:
			if uuidPattern.MatchString(args[i]) {
				out[p.names[0]] = args[i]
			} else {
				out[p.names[1]] = args[i]
			}
			i++
		default:
			out[p.names[0]] = args[i]
			i++
		}
	}
	for _, p := range flagArgs {
		name := c.flagName(p.Name)
		if !cmd.Flags().Changed(name) {
			continue
		}
		if c.Pairs[p.Name] {
			vals, _ := cmd.Flags().GetStringArray(name)
			m, err := pairsArg(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), name, vals)
			if err != nil {
				return nil, err
			}
			out[p.Name] = m
			continue
		}
		var raw string
		if c.Files[p.Name] {
			path := cmd.Flags().Lookup(name).Value.String()
			data, err := readArgFile(cmd, path)
			if err != nil {
				return nil, clerrors.Wrap(clerrors.CodeUsage, fmt.Sprintf("--%s %s: %v", name, path, err), "", err)
			}
			v, err := coerceArg(p, string(data))
			if err != nil {
				return nil, clerrors.New(clerrors.CodeUsage, fmt.Sprintf("--%s %s: %v", name, path, err), "")
			}
			out[p.Name] = v
			continue
		}
		if p.Type == "array" {
			vals, _ := cmd.Flags().GetStringArray(name)
			raw = strings.Join(vals, ",")
		} else {
			raw = cmd.Flags().Lookup(name).Value.String()
		}
		v, err := coerceArg(p, raw)
		if err != nil {
			return nil, clerrors.New(clerrors.CodeUsage, fmt.Sprintf("--%s: %v", name, err), "")
		}
		out[p.Name] = v
	}
	var missing []string
	for _, p := range schemaParams(t) {
		if _, ok := out[p.Name]; p.Required && !ok {
			missing = append(missing, "--"+c.flagName(p.Name))
		}
	}
	if len(missing) > 0 {
		return nil, clerrors.New(clerrors.CodeUsage,
			i18n.Tf("%s: missing required flag(s): %s", "%s: не заданы обязательные флаги: %s", cmd.CommandPath(), strings.Join(missing, ", ")),
			helpHint(cmd))
	}
	return out, nil
}

// pairsArg turns repeated key=value flags into an object. The value is
// everything after the first "=", so it may contain "=" itself. A value
// that is "-" is read from stdin (one key at most), "@path" from a file;
// a trailing newline is trimmed from both. A secret given so stays out of
// argv and the shell history; a literal value that starts with "@" (or is
// "-") goes through a file.
func pairsArg(ctx context.Context, stdin io.Reader, prompt io.Writer, flag string, vals []string) (map[string]any, error) {
	out := make(map[string]any, len(vals))
	fromStdin := ""
	for _, v := range vals {
		k, val, ok := strings.Cut(v, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, clerrors.New(clerrors.CodeUsage,
				i18n.Tf("--%s %q: want key=value", "--%s %q: нужно поле=значение", flag, v),
				i18n.Tf("Example: --%[1]s hostname=web-01; a password from a file or stdin: --%[1]s admin_password=@./admin.pass or --%[1]s admin_password=-; the fields of an image: evo image get <image>.",
					"Пример: --%[1]s hostname=web-01; пароль — из файла или stdin: --%[1]s admin_password=@./admin.pass или --%[1]s admin_password=-; поля образа — evo image get <образ>.", flag))
		}
		if _, dup := out[k]; dup {
			return nil, clerrors.New(clerrors.CodeUsage,
				i18n.Tf("--%s %s is given twice", "--%s %s задан дважды", flag, k), "")
		}
		switch {
		case val == "-":
			if fromStdin != "" {
				return nil, clerrors.New(clerrors.CodeUsage,
					i18n.Tf("--%s: %s and %s both read stdin", "--%s: %s и %s оба читают stdin", flag, fromStdin, k),
					i18n.Tf("Only one field may read stdin; give the others from files: --%s %s=@<file>.",
						"stdin читает только одно поле; остальные — из файлов: --%s %s=@<файл>.", flag, k))
			}
			fromStdin = k
			if f, ok := stdin.(*os.File); ok && f == os.Stdin && stdinIsTerminal() {
				// A terminal: one line, not echoed, with a prompt — not a
				// silent wait for Ctrl-D with the password on the screen.
				if ctx == nil {
					ctx = context.Background()
				}
				v, err := newConsole(os.Stdin, prompt, true).askSecret(ctx,
					i18n.Tf("Value of %s (not echoed): ", "Значение поля %s (не отображается): ", k))
				if err != nil {
					return nil, err
				}
				val = v
				break
			}
			b, err := io.ReadAll(stdin)
			if err != nil {
				return nil, clerrors.New(clerrors.CodeUsage,
					i18n.Tf("--%s %s=-: stdin was not read: %v", "--%s %s=-: stdin не прочитан: %v", flag, k, err), "")
			}
			val = trimNewline(string(b))
		case strings.HasPrefix(val, "@"):
			path := val[1:]
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, clerrors.New(clerrors.CodeUsage,
					i18n.Tf("--%s %s=@%s: the file was not read: %v", "--%s %s=@%s: файл не прочитан: %v", flag, k, path, err),
					i18n.T("A value that starts with @ is a file name; a literal one goes through a file too.",
						"Значение, которое начинается с @, — имя файла; такое значение буквально тоже передаётся через файл."))
			}
			val = trimNewline(string(b))
		}
		out[k] = val
	}
	return out, nil
}

// trimNewline drops one trailing newline ("\n" or "\r\n"): the one an
// editor or echo adds.
func trimNewline(s string) string {
	if t, ok := strings.CutSuffix(s, "\r\n"); ok {
		return t
	}
	return strings.TrimSuffix(s, "\n")
}

// readArgFile reads the file of a Files flag; "-" is stdin.
func readArgFile(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(path)
}

func helpHint(cmd *cobra.Command) string {
	return i18n.Tf("Run '%s --help'.", "Справка: '%s --help'.", cmd.CommandPath())
}

// --- answer formatting ---

// tableView is how renderAnswer lays out a list: its columns, whether the
// table is for a Russian tree, and the Russian column titles of that tree
// beyond columnHeaderRU.
type tableView struct {
	Columns []string
	Russian bool
	Headers map[string]string
}

// ru: the table speaks Russian (a Russian tree, and the CLI in Russian).
func (v tableView) ru() bool { return v.Russian && i18n.Lang() == i18n.RU }

// header is the title of a column.
func (v tableView) header(col string) string {
	if v.ru() {
		if h, ok := v.Headers[col]; ok {
			return h
		}
		if h, ok := columnHeaderRU[col]; ok {
			return h
		}
	}
	return strings.ToUpper(col)
}

// cell is a table cell: a Russian table says да/нет, not true/false.
func (v tableView) cell(x any, max int) string {
	if b, ok := x.(bool); ok && v.ru() {
		return yesNo(b)
	}
	return cell(x, max)
}

// note is a line under a list.
func (v tableView) note(k string, x any) string {
	if v.ru() {
		if s, ok := x.(string); ok {
			if ru, ok := listNoteRU[s]; ok {
				return ru
			}
		}
		if label, ok := noteLabelRU[k]; ok {
			return fmt.Sprintf("%s: %v", label, x)
		}
	}
	return fmt.Sprintf("%s: %v", k, x)
}

// renderAnswer shows a tool's JSON answer: a list as a table, an object as
// FIELD/VALUE rows. Anything else is printed as is.
func renderAnswer(p *output.Printer, w io.Writer, text string, view tableView) error {
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		fmt.Fprintln(w, text)
		return nil
	}
	if rows, notes, ok := findList(v); ok {
		renderList(p, w, rows, view)
		for _, k := range sortedKeys(notes) {
			fmt.Fprintln(w, view.note(k, notes[k]))
		}
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		fmt.Fprintln(w, text)
		return nil
	}
	// A resource (it has an id or a name) is one FIELD/VALUE table. A
	// composite answer without one (k8s_options: versions, flavors, zones)
	// shows each list of objects as its own table under the fields.
	_, hasID := m["id"]
	_, hasName := m["name"]
	sections := map[string][]any{}
	if !hasID && !hasName {
		for k, x := range m {
			if a, ok := x.([]any); ok && len(a) > 0 {
				if _, obj := a[0].(map[string]any); obj {
					sections[k] = a
				}
			}
		}
	}
	keys := sortedKeys(m)
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		if _, sec := sections[k]; !sec {
			rows = append(rows, []string{k, view.cell(m[k], 0)})
		}
	}
	if len(rows) > 0 {
		p.Table([]string{i18n.T("FIELD", "ПОЛЕ"), i18n.T("VALUE", "ЗНАЧЕНИЕ")}, rows)
	}
	printed := len(rows) > 0
	for _, k := range keys {
		if a, sec := sections[k]; sec {
			if printed {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s:\n", strings.ToUpper(k))
			renderList(p, w, a, tableView{Russian: view.Russian, Headers: view.Headers})
			printed = true
		}
	}
	return nil
}

// listNoteFields are the text fields a list answer may carry next to its
// list (list_flavors: a note on prices); they are printed under the table.
var listNoteFields = map[string]bool{"note": true, "warning": true, "hint": true}

// findList returns the rows of a list answer: a top-level array, or the
// only array field of an object whose other fields are numbers (count,
// total) or notes. An object with other fields (vm_get: name, private_ip
// and an interfaces array) is a single resource, not a list.
func findList(v any) ([]any, map[string]any, bool) {
	if a, ok := v.([]any); ok {
		return a, nil, true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil, false
	}
	var list []any
	notes := map[string]any{}
	found := 0
	for k, x := range m {
		switch x := x.(type) {
		case []any:
			list = x
			found++
		case float64:
		case string:
			if !listNoteFields[k] {
				return nil, nil, false
			}
			notes[k] = x
		default:
			return nil, nil, false
		}
	}
	return list, notes, found == 1
}

// preferredColumns come first when the columns are picked from the rows.
var preferredColumns = []string{"id", "name", "path", "key", "status", "state", "phase", "version", "size", "zone"}

const maxAutoColumns = 7

func renderList(p *output.Printer, w io.Writer, rows []any, view tableView) {
	if len(rows) == 0 {
		fmt.Fprintln(w, i18n.T("No items.", "Пусто."))
		return
	}
	if _, isObj := rows[0].(map[string]any); !isObj {
		out := make([][]string, len(rows))
		for i, r := range rows {
			out[i] = []string{view.cell(r, 0)}
		}
		p.Table([]string{i18n.T("VALUE", "ЗНАЧЕНИЕ")}, out)
		return
	}
	columns := view.Columns
	if len(columns) == 0 {
		columns = autoColumns(rows)
	}
	headers := make([]string, len(columns))
	for i, c := range columns {
		headers[i] = view.header(c)
	}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		m, _ := r.(map[string]any)
		line := make([]string, len(columns))
		for i, c := range columns {
			line[i] = view.cell(m[c], 40)
		}
		out = append(out, line)
	}
	p.Table(headers, out)
}

// autoColumns picks up to maxAutoColumns keys with scalar values from the
// first row: the preferred ones first, then the rest by name.
func autoColumns(rows []any) []string {
	first, _ := rows[0].(map[string]any)
	scalar := map[string]bool{}
	for k, v := range first {
		switch v.(type) {
		case map[string]any, []any:
			if named, ok := v.(map[string]any); ok && named["name"] != nil {
				scalar[k] = true // shown by its name
			}
		default:
			scalar[k] = true
		}
	}
	var cols []string
	for _, k := range preferredColumns {
		if scalar[k] {
			cols = append(cols, k)
			delete(scalar, k)
		}
	}
	rest := make([]string, 0, len(scalar))
	for k := range scalar {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	cols = append(cols, rest...)
	if len(cols) > maxAutoColumns {
		cols = cols[:maxAutoColumns]
	}
	return cols
}

// cell renders a value for a table; max > 0 cuts it to that many runes.
func cell(v any, max int) string {
	var s string
	switch x := v.(type) {
	case nil:
		s = ""
	case string:
		s = x
	case bool, float64:
		b, _ := json.Marshal(x)
		s = string(b)
	case map[string]any:
		if n, ok := x["name"].(string); ok && max > 0 {
			s = n
		} else {
			b, _ := json.Marshal(x)
			s = string(b)
		}
	case []any:
		parts := make([]string, 0, len(x))
		simple := true
		for _, e := range x {
			switch e.(type) {
			case map[string]any, []any:
				simple = false
			}
			parts = append(parts, cell(e, 0))
		}
		if simple {
			s = strings.Join(parts, ",")
		} else {
			b, _ := json.Marshal(x)
			s = string(b)
		}
	default:
		b, _ := json.Marshal(x)
		s = string(b)
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); max > 0 && len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
