// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Tab completes the values of the arguments that name a resource or an
// entry of a catalogue (release plan 1.5): which ones, and from which list,
// the registry decides (tools.Registry.Completes, Complete). Every command
// registers its completions here, from that one declaration — the trees,
// the engine commands and the root's scope flags alike.

// completeValues is the completion of the argument arg of tool; tool ""
// completes by the argument's name alone (the root's --region,
// --project-id). The registry is the run's, read when the completion runs:
// cobra keeps the functions of flags in a table of the process, and one
// that held a registry would keep it for good.
func completeValues(tool, arg string) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		var out []cobra.Completion
		for _, c := range registryOf(cmd).Complete(ctx, tool, arg, toComplete) {
			if c.Note == "" {
				out = append(out, c.Value)
				continue
			}
			out = append(out, cobra.CompletionWithDesc(c.Value, c.Note))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// registerFlagCompletions completes each flag of cmd whose argument of
// tool the registry completes; flags maps a flag to its tool argument.
func registerFlagCompletions(reg *tools.Registry, cmd *cobra.Command, tool string, flags map[string]string) {
	for flag, arg := range flags {
		if cmd.Flags().Lookup(flag) != nil && reg.Completes(tool, arg) {
			_ = cmd.RegisterFlagCompletionFunc(flag, completeValues(tool, arg))
		}
	}
}

// treeCompletion is Tab on an argument of a tree command (#718): the
// positional at the cursor, by the first of its names the registry
// completes ("vm_id|name": vm_id), the shell's files only where the
// registry declares it a path (CLISpec.Files, CLISpec.Paths). Past the positionals, on an
// empty word, the flags the call still lacks — the required first, then
// the others — with a line of ActiveHelp: what is still required, or that
// the call is ready to run.
func treeCompletion(reg *tools.Registry, t tools.Tool, pos []positional, flags map[string]string) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) < len(pos) && !pos[len(args)].rest {
			for _, name := range pos[len(args)].names {
				if reg.Completes(t.Name, name) {
					return completeValues(t.Name, name)(cmd, args, toComplete)
				}
			}
			for _, name := range pos[len(args)].names {
				if t.CLI.Files[name] || t.CLI.Paths[name] {
					return nil, cobra.ShellCompDirectiveDefault
				}
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		if toComplete != "" || (len(pos) > 0 && pos[len(pos)-1].rest) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return flagsToGo(cmd, t, flags), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	}
}

// tabNeeds are the arguments a call of t must have: alone (the schema's
// required) and in sets one of which is enough (RequiredOneOf). The
// consent is no flag of the command (--yes), so it is never among them.
func tabNeeds(t tools.Tool) (alone map[string]bool, oneOf [][]string) {
	alone = map[string]bool{}
	if req, ok := t.InputSchema["required"].([]string); ok {
		for _, a := range req {
			alone[a] = true
		}
	}
	if req, ok := t.InputSchema["required"].([]any); ok {
		for _, a := range req {
			if s, ok := a.(string); ok {
				alone[s] = true
			}
		}
	}
	return alone, t.Decl.RequiredOneOf
}

// flagsToGo lists, as candidates of Tab, the flags of cmd's tool arguments
// not given yet, in the order of the help. cobra puts the flags of
// markTabRequired before them itself (unless given), so they are not
// repeated here; an ActiveHelp line names what is still needed — a set of
// RequiredOneOf as one entry, gone once one of its flags is given — or
// says the call is ready.
func flagsToGo(cmd *cobra.Command, t tools.Tool, flags map[string]string) []cobra.Completion {
	alone, oneOf := tabNeeds(t)
	flagOf := map[string]string{}
	for f, a := range flags {
		flagOf[a] = f
	}
	given := func(arg string) bool {
		f := cmd.Flags().Lookup(flagOf[arg])
		return f != nil && f.Changed
	}
	var missing []string
	for _, set := range oneOf {
		var names []string
		met := false
		for _, a := range set {
			if flagOf[a] != "" {
				names = append(names, "--"+flagOf[a])
			}
			met = met || given(a)
		}
		if !met && len(names) > 0 {
			missing = append(missing, strings.Join(names, " | "))
		}
	}
	var out []cobra.Completion
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		arg, ok := flags[f.Name]
		if !ok || f.Hidden || f.Changed {
			return
		}
		if alone[arg] {
			missing = append(missing, "--"+f.Name)
		}
		if _, marked := f.Annotations[cobra.BashCompOneRequiredFlag]; marked {
			return
		}
		out = append(out, cobra.CompletionWithDesc("--"+f.Name, f.Usage))
	})
	if cobra.GetActiveHelpConfig(cmd) == "0" {
		return out
	}
	if len(missing) > 0 {
		return cobra.AppendActiveHelp(out, i18n.T("still required: ", "ещё обязательно: ")+strings.Join(missing, ", "))
	}
	return cobra.AppendActiveHelp(out, i18n.T("ready to run: Enter, or --dry-run to see the preview", "готово к запуску: Enter или --dry-run, чтобы увидеть превью"))
}

// registerFlagHints completes the value of each flag of cmd's tool
// arguments that the registry does not complete: files where it declares a
// path (CLISpec.Files, CLISpec.Paths), the closed list of the schema (enum), nothing
// otherwise — a free value (a name, a size, a port) — with its description
// and bounds as ActiveHelp, as the help shows them from the schema.
func registerFlagHints(reg *tools.Registry, cmd *cobra.Command, t tools.Tool, flags map[string]string) {
	for flag, arg := range flags {
		f := cmd.Flags().Lookup(flag)
		if f == nil || reg.Completes(t.Name, arg) || f.Value.Type() == "bool" {
			continue
		}
		if t.CLI.Files[arg] || t.CLI.Paths[arg] {
			_ = cmd.RegisterFlagCompletionFunc(flag, func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
				return nil, cobra.ShellCompDirectiveDefault
			})
			continue
		}
		if values := schemaEnum(t, arg); len(values) > 0 {
			_ = cmd.RegisterFlagCompletionFunc(flag, cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp))
			continue
		}
		usage := f.Usage
		_ = cmd.RegisterFlagCompletionFunc(flag, func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
			var out []cobra.Completion
			if cobra.GetActiveHelpConfig(cmd) != "0" {
				out = cobra.AppendActiveHelp(out, usage)
			}
			return out, cobra.ShellCompDirectiveNoFileComp
		})
	}
}

// tabRequired is the value of cobra's "required" annotation that evo puts
// on the flags a call needs: cobra's Tab on "-" offers the annotated flags
// first and alone while any is unset, and only the value "true" makes cobra
// refuse the run itself — evo's refusal stays the tool's (tools.CheckArgs),
// one check on both surfaces (#718, T2).
const tabRequired = "evo"

// markTabRequired annotates the flags of cmd whose arguments a call of t
// must have — the schema's required and the sets of RequiredOneOf, as the
// registry declares them; flags maps a flag to its tool argument.
func markTabRequired(cmd *cobra.Command, t tools.Tool, flags map[string]string) {
	need, oneOf := tabNeeds(t)
	for _, set := range oneOf {
		for _, a := range set {
			need[a] = true
		}
	}
	for flag, arg := range flags {
		if need[arg] && cmd.Flags().Lookup(flag) != nil {
			_ = cmd.Flags().SetAnnotation(flag, cobra.BashCompOneRequiredFlag, []string{tabRequired})
		}
	}
}

// tabNoFiles is the one point of Tab's default (#718, T1): a command that
// does not complete its arguments itself says there is nothing to complete
// (ShellCompDirectiveNoFileComp) instead of cobra's fallback to the
// shell's files. Files come only where the registry declares a path
// (positionalCompletion).
func tabNoFiles(c *cobra.Command) {
	if c.ValidArgsFunction == nil && len(c.ValidArgs) == 0 {
		c.ValidArgsFunction = cobra.NoFileCompletions
	}
	for _, s := range c.Commands() {
		tabNoFiles(s)
	}
}

// registerEngineCompletions completes the flags of the engine commands
// (evo deploy) and the root's scope flags.
func registerEngineCompletions(root *cobra.Command, reg *tools.Registry) {
	for _, c := range engineCommands {
		if c.Alias != "" {
			continue
		}
		cmd, _, err := root.Find(strings.Fields(strings.TrimPrefix(c.Path, "evo ")))
		if err != nil || cmd == root {
			continue
		}
		registerFlagCompletions(reg, cmd, c.Tool, c.Flags)
	}
	for _, flag := range []string{"region", "project-id"} {
		_ = root.RegisterFlagCompletionFunc(flag, completeValues("", strings.ReplaceAll(flag, "-", "_")))
	}
}

// zshMenuStyle is what evo completion zsh adds before the script calls _evo
// (#718, T4): the arrows choose among the candidates of evo, as Tab Tab
// does in bash — only for evo, and only when the person has no style of
// their own for it (zstyle -L reads it).
const zshMenuStyle = `# evo: choose a candidate with the arrows, unless you have your own style for evo.
if [[ -z "$(zstyle -L ':completion:*:*:evo:*' menu)" ]]; then
    zstyle ':completion:*:*:evo:*' menu yes select
fi

`

// zshCallMark starts the end of cobra's zsh script: the call of _evo when
// the script is loaded as the function's file.
const zshCallMark = "# don't run the completion function when being source-ed or eval-ed"

// zshMenuOfEvo makes evo completion zsh (a subcommand of completion) write
// zshMenuStyle into cobra's script, before its call of _evo: so the first
// Tab already has the menu. cobra's own run writes to the output the
// command had when it was made, so the script is generated here.
func zshMenuOfEvo(completion *cobra.Command) {
	for _, z := range completion.Commands() {
		if z.Name() != "zsh" {
			continue
		}
		z.RunE = func(cmd *cobra.Command, _ []string) error {
			var buf bytes.Buffer
			gen := cmd.Root().GenZshCompletion
			if noDesc, _ := cmd.Flags().GetBool("no-descriptions"); noDesc {
				gen = cmd.Root().GenZshCompletionNoDesc
			}
			if err := gen(&buf); err != nil {
				return err
			}
			script := buf.String()
			if i := strings.Index(script, zshCallMark); i >= 0 {
				script = script[:i] + zshMenuStyle + script[i:]
			} else {
				script += "\n" + zshMenuStyle
			}
			_, err := cmd.OutOrStdout().Write([]byte(script))
			return err
		}
	}
}

// schemaEnum is the closed list of values the schema of t gives arg.
func schemaEnum(t tools.Tool, arg string) []string {
	props, _ := t.InputSchema["properties"].(map[string]any)
	p, _ := props[arg].(map[string]any)
	var out []string
	switch e := p["enum"].(type) {
	case []any:
		for _, v := range e {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = e
	}
	return out
}
