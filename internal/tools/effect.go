// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

// Effect is what a call of a tool does besides answering: the one decision
// the hints of MCP (readOnlyHint, destructiveHint) and the catalog of tools
// (docs/tools.md, column «Меняет») take, so that neither calls a read what
// the other does not (#1744).
type Effect int

const (
	// EffectRead: the tool changes nothing and reveals no secret.
	EffectRead Effect = iota
	// EffectSideEffects: the tool takes no consent but is not a read
	// (HasSideEffects: stack_status advances the stack, k8s_kubeconfig
	// mints a token of the cluster).
	EffectSideEffects
	// EffectSecret: the tool changes nothing but reveals a secret in
	// plaintext and takes consent (IsSensitive).
	EffectSecret
	// EffectMutating: the tool creates or changes resources and takes
	// consent (Tool.Mutating).
	EffectMutating
	// EffectDestructive: a mutating tool that may delete or overwrite data
	// (IsDestructive).
	EffectDestructive
)

// EffectOf is the Effect of t.
func EffectOf(t Tool) Effect {
	switch {
	case IsSensitive(t):
		return EffectSecret
	case HasSideEffects(t):
		return EffectSideEffects
	case IsDestructive(t):
		return EffectDestructive
	case t.Mutating:
		return EffectMutating
	}
	return EffectRead
}
