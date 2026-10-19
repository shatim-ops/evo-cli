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

// The command trees (vm, image, marketplace, disk, vpc, k8s, pg, s3, secret) are data: each
// command names a registry tool, which of its arguments are positional, and
// how the answer is shown. Every other argument of the tool becomes a flag
// with the type from its schema. The cobra layer only parses flags into
// map[string]any, calls the tool and formats the answer.

// treeGroup is one top-level command.
type treeGroup struct {
	Name  string
	Short string
	Cmds  []treeCmd
}

// treeCmd is one command of a tree.
//
// Args lists the positional arguments as tool argument names:
//
//	"disk_id"     required
//	"id?"         optional
//	"args..."     the rest of the command line, joined with spaces
//	"vm_id|name"  required; goes to vm_id when it is a UUID, to name otherwise
type treeCmd struct {
	Verb   string
	Tool   string
	Args   []string
	Rename map[string]string // tool argument → flag name, when the default clashes or reads badly
	// Omit are tool arguments that get no flag: an alias of a positional
	// argument (pg get <cluster-id> beside the tool's id). With both, the
	// flag won and the positional was lost: `pg delete A --id B` deleted B
	// (QA 20.10, QA-1).
	Omit map[string]bool
	// Pairs are object arguments given as a repeatable key=value flag
	// (--meta admin_password=…) instead of one JSON object.
	Pairs   map[string]bool
	Columns []string // table columns for a list answer (default: picked from the rows)
	// russian: the human table of a Russian tree (vm, disk) speaks
	// Russian — column names, да/нет, the note under a list. Set from
	// russianTrees; --json keeps the tool's answer.
	russian bool
	// Render shows the answer instead of the generic table (image list,
	// image get); --json still prints the tool's answer as is.
	Render func(p *output.Printer, w io.Writer, text string) error
	// Raw prints the answer's "output" (what kubectl or helm printed) as
	// is, not as a FIELD/VALUE table; on a tool error it goes to stderr
	// before the error.
	Raw bool
	// Files are tool arguments whose flag names a file holding the value
	// ("-": stdin), read and checked against the argument's type:
	// k8s add-addon --config settings.json.
	Files map[string]bool
}

var treeGroups = []treeGroup{
	{Name: "vm", Short: "Virtual machines", Cmds: []treeCmd{
		{Verb: "list", Tool: "vm_list", Columns: []string{"id", "name", "state", "zone", "created_time"}},
		{Verb: "get", Tool: "vm_get", Args: []string{"vm_id|name"}},
		{Verb: "create", Tool: "provision", Args: []string{"name"},
			Rename: map[string]string{"image_metadata": "meta", "ports": "port"},
			Pairs:  map[string]bool{"image_metadata": true}},
		{Verb: "delete", Tool: "destroy", Args: []string{"vm_id|name"}},
		{Verb: "zones", Tool: "list_zones"},
		{Verb: "flavors", Tool: "list_flavors", Columns: []string{"name", "vcpus", "ram_gb", "est_monthly_rub", "free_tier"}},
	}},
	{Name: "image", Short: "VM images: operating systems and Marketplace products", Cmds: []treeCmd{
		{Verb: "list", Tool: "image_list", Args: []string{"query?"}, Render: renderImageList},
		{Verb: "get", Tool: "image_get", Args: []string{"image_id|image"}, Render: renderImageCard},
	}},
	{Name: "marketplace", Short: "Showcase: VM images and Kubernetes addons, with the command that deploys each", Cmds: []treeCmd{
		{Verb: "list", Tool: "marketplace_list", Args: []string{"query?"}, Render: renderMarketplaceList},
	}},
	{Name: "disk", Short: "Disks", Cmds: []treeCmd{
		{Verb: "list", Tool: "disk_list", Columns: []string{"id", "name", "size_gb", "state", "disk_type", "zone", "attached_to"}},
		{Verb: "types", Tool: "list_disk_types"},
		{Verb: "create", Tool: "disk_create", Args: []string{"name"}},
		{Verb: "attach", Tool: "disk_attach", Args: []string{"disk_id"}},
		{Verb: "detach", Tool: "disk_detach", Args: []string{"disk_id"}},
		{Verb: "resize", Tool: "disk_resize", Args: []string{"disk_id"}},
		{Verb: "delete", Tool: "disk_delete", Args: []string{"disk_id"}},
	}},
	{Name: "vpc", Short: "Networks: VPCs, routes, subnets, security groups, floating IPs", Cmds: []treeCmd{
		{Verb: "list", Tool: "vpc_list"},
		{Verb: "get", Tool: "vpc_get", Args: []string{"id"}},
		{Verb: "create", Tool: "vpc_create", Args: []string{"name"}},
		{Verb: "delete", Tool: "vpc_delete", Args: []string{"id"}},
		{Verb: "operation", Tool: "vpc_operation", Args: []string{"operation_id"}},
		{Verb: "routes", Tool: "vpc_routes", Args: []string{"vpc_id"}},
		{Verb: "route-create", Tool: "vpc_route_create", Args: []string{"vpc_id"}},
		{Verb: "route-delete", Tool: "vpc_route_delete", Args: []string{"vpc_id", "route_id"}},
		{Verb: "subnets", Tool: "list_subnets"},
		{Verb: "security-groups", Tool: "list_security_groups"},
		{Verb: "fips", Tool: "list_floating_ips"},
		{Verb: "fip-attach", Tool: "fip_attach", Args: []string{"fip"}},
		{Verb: "fip-release", Tool: "release_floating_ip", Args: []string{"id?"}},
	}},
	{Name: "k8s", Short: "Managed Kubernetes clusters", Cmds: []treeCmd{
		{Verb: "list", Tool: "k8s_list"},
		{Verb: "get", Tool: "k8s_get", Args: []string{"cluster_id"}},
		{Verb: "options", Tool: "k8s_options"},
		{Verb: "plan", Tool: "k8s_plan"},
		{Verb: "create", Tool: "k8s_create"},
		{Verb: "delete", Tool: "k8s_delete", Args: []string{"cluster_id"}},
		{Verb: "kubeconfig", Tool: "k8s_kubeconfig", Args: []string{"cluster_id"}},
		{Verb: "nodepools", Tool: "k8s_nodepools", Args: []string{"cluster_id"}},
		{Verb: "add-nodepool", Tool: "k8s_add_nodepool", Args: []string{"cluster_id"}},
		{Verb: "addons", Tool: "k8s_addons", Args: []string{"query?"}, Render: renderAddonList},
		{Verb: "addon-versions", Tool: "k8s_addon_versions", Args: []string{"name"}, Render: renderAddonVersions},
		{Verb: "add-addon", Tool: "k8s_add_addon", Args: []string{"cluster_id", "name"}, Files: map[string]bool{"config": true}},
		{Verb: "kubectl", Tool: "k8s_kubectl", Args: []string{"cluster_id", "args..."}, Raw: true},
		{Verb: "apply", Tool: "k8s_apply", Args: []string{"cluster_id"}, Raw: true},
		{Verb: "helm", Tool: "k8s_helm", Args: []string{"cluster_id", "action"}, Raw: true},
	}},
	{Name: "pg", Short: "Managed PostgreSQL clusters, databases, users, backups", Cmds: []treeCmd{
		{Verb: "list", Tool: "pg_list"},
		{Verb: "get", Tool: "pg_get", Args: []string{"cluster_id"}, Omit: map[string]bool{"id": true}},
		{Verb: "versions", Tool: "pg_versions"},
		{Verb: "specs", Tool: "pg_specs"},
		{Verb: "create", Tool: "pg_create", Args: []string{"name"}},
		{Verb: "delete", Tool: "pg_delete", Args: []string{"cluster_id"}, Omit: map[string]bool{"id": true}},
		{Verb: "operation", Tool: "pg_operation", Args: []string{"operation_id"}},
		{Verb: "databases", Tool: "pg_databases", Args: []string{"cluster_id"}},
		{Verb: "db-create", Tool: "pg_db_create", Args: []string{"cluster_id", "name"}},
		{Verb: "db-delete", Tool: "pg_db_delete", Args: []string{"cluster_id", "name"}},
		{Verb: "users", Tool: "pg_users", Args: []string{"cluster_id"}},
		{Verb: "user-create", Tool: "pg_user_create", Args: []string{"cluster_id", "name"}},
		{Verb: "user-delete", Tool: "pg_user_delete", Args: []string{"cluster_id", "name"}},
		{Verb: "backups", Tool: "pg_backups", Args: []string{"cluster_id"}},
		{Verb: "backup-create", Tool: "pg_backup_create", Args: []string{"cluster_id", "name"}},
	}},
	{Name: "s3", Short: "Object Storage: buckets and objects", Cmds: []treeCmd{
		{Verb: "buckets", Tool: "s3_buckets"},
		{Verb: "ls", Tool: "s3_ls", Args: []string{"bucket"}},
		{Verb: "get", Tool: "s3_get", Args: []string{"bucket", "key"}},
		{Verb: "put", Tool: "s3_put", Args: []string{"bucket", "key"}},
		{Verb: "rm", Tool: "s3_rm", Args: []string{"bucket", "key"}},
		{Verb: "bucket-create", Tool: "s3_bucket_create", Args: []string{"bucket"},
			Rename: map[string]string{"region": "s3-region"}},
		{Verb: "bucket-delete", Tool: "s3_bucket_delete", Args: []string{"bucket"}},
	}},
	{Name: "secret", Short: "Secret Manager", Cmds: []treeCmd{
		{Verb: "list", Tool: "secret_list"},
		{Verb: "get", Tool: "secret_get", Args: []string{"path"}},
		{Verb: "value", Tool: "secret_value", Args: []string{"path"}},
		{Verb: "create", Tool: "secret_create", Args: []string{"path"}},
		{Verb: "add-version", Tool: "secret_add_version", Args: []string{"path"}},
		{Verb: "delete", Tool: "secret_delete", Args: []string{"path"}},
	}},
}

// rootArgs are tool arguments served by root flags (--project-id,
// --region) through the engine configuration, and confirmed, which is
// given with --yes or at the prompt.
var rootArgs = map[string]bool{"project_id": true, "region": true, "confirmed": true}

// newTreeCmds builds the command trees from treeGroups.
func newTreeCmds() []*cobra.Command {
	reg := newRegistry()
	var out []*cobra.Command
	for _, g := range treeGroups {
		short := g.Short
		if ru, ok := treeGroupShortRU[g.Name]; ok {
			short = i18n.T(g.Short, ru)
		}
		group := asGroup(&cobra.Command{Use: g.Name, Short: short})
		for _, c := range g.Cmds {
			t, ok := reg.Lookup(c.Tool)
			if !ok {
				panic("command tree: no tool " + c.Tool) // caught by TestTrees_MatchRegistry
			}
			c.russian = russianTrees[g.Name]
			group.AddCommand(newTreeLeaf(c, t))
		}
		out = append(out, group)
	}
	return out
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
		if ru, ok := toolShortRU[t.Name]; ok {
			short = ru
		}
		runs := "Вызывает инструмент " + t.Name + " (подробнее: 'evo tools describe " + t.Name + "')."
		if ru, ok := toolLongRU[t.Name]; ok {
			long = short + ".\n\n" + ru + "\n\n" + runs
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
		Use:   strings.Join(use, " "),
		Short: short,
		Long:  long,
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
		if c.Render != nil {
			return c.Render(cmdPrinter(cmd), cmd.OutOrStdout(), text)
		}
		return renderAnswer(cmdPrinter(cmd), cmd.OutOrStdout(), text, tableView{Columns: c.Columns, Russian: c.russian})
	}
	return cmd
}

// defaultTrueRE is the "(default: true)" of a schema description: pflag
// adds its own "(default true)" to the help of a flag that defaults to true.
var defaultTrueRE = regexp.MustCompile(` ?\(default:? true\)`)

// flagDesc is the help of the flag for argument p: the Russian text from
// flagShortRU when the CLI speaks Russian and the map has one (ru is then
// true), the schema's English description otherwise.
func flagDesc(t tools.Tool, p param) (desc string, ru bool) {
	if i18n.Lang() == i18n.RU {
		if s, ok := flagShortRU[t.Name+"."+p.Name]; ok {
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

// tableView is how renderAnswer lays out a list: its columns and whether
// the table is for a Russian tree.
type tableView struct {
	Columns []string
	Russian bool
}

// ru: the table speaks Russian (a Russian tree, and the CLI in Russian).
func (v tableView) ru() bool { return v.Russian && i18n.Lang() == i18n.RU }

// header is the title of a column.
func (v tableView) header(col string) string {
	if h, ok := columnHeaderRU[col]; ok && v.ru() {
		return h
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
			renderList(p, w, a, tableView{Russian: view.Russian})
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
