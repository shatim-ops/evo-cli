// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
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
// completes ("vm_id|name": vm_id).
func positionalCompletion(reg *tools.Registry, tool string, pos []positional) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) >= len(pos) || pos[len(args)].rest {
			return nil, cobra.ShellCompDirectiveDefault
		}
		for _, name := range pos[len(args)].names {
			if reg.Completes(tool, name) {
				return completeValues(tool, name)(cmd, args, toComplete)
			}
		}
		return nil, cobra.ShellCompDirectiveDefault
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
