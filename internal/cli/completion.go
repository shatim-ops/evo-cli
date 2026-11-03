// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Tab completes the values of the arguments that name a resource or an
// entry of a catalogue (release plan 1.5): which ones, and from which list,
// the registry decides (tools.Registry.Completes, Complete). Every command
// registers its completions here, from that one declaration — the trees,
// the engine commands and the root's scope flags alike.

// completeValues is the completion of the argument arg of tool; tool ""
// completes by the argument's name alone (the root's --region,
// --project-id).
func completeValues(tool, arg string) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		var out []cobra.Completion
		for _, c := range newRegistry().Complete(ctx, tool, arg, toComplete) {
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

// positionalCompletion completes the positional arguments of a tree
// command: the one at the cursor, by the first of its names the registry
// completes ("vm_id|name": vm_id). A positional the registry declares a
// path (CLISpec.Files) gets the shell's files; any other, nothing to
// complete gets no files either (tabNoFiles).
func positionalCompletion(reg *tools.Registry, t tools.Tool, pos []positional) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) >= len(pos) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		for _, name := range pos[len(args)].names {
			if reg.Completes(t.Name, name) {
				return completeValues(t.Name, name)(cmd, args, toComplete)
			}
		}
		for _, name := range pos[len(args)].names {
			if t.CLI.Files[name] {
				return nil, cobra.ShellCompDirectiveDefault
			}
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
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
	need := map[string]bool{}
	if req, ok := t.InputSchema["required"].([]string); ok {
		for _, a := range req {
			need[a] = true
		}
	}
	if req, ok := t.InputSchema["required"].([]any); ok {
		for _, a := range req {
			if s, ok := a.(string); ok {
				need[s] = true
			}
		}
	}
	for _, set := range t.RequiredOneOf {
		for _, a := range set {
			need[a] = true
		}
	}
	for flag, arg := range flags {
		if need[arg] && arg != "confirmed" && cmd.Flags().Lookup(flag) != nil {
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
func registerEngineCompletions(root *cobra.Command) {
	reg := newRegistry()
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
