// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// refKind is what a reference argument names: its kind in both languages
// and the list tool that shows the project's ones.
type refKind struct{ en, ru, list string }

var (
	refVPC           = refKind{"VPC", "VPC", "vpc_list"}
	refSubnet        = refKind{"subnet", "подсеть", "list_subnets"}
	refSecurityGroup = refKind{"security group", "группа безопасности", "list_security_groups"}
)

// refByName is the id a reference argument names (D57): a value of the UUID
// form, or an id of items, is an id, returned as it is; any other value is
// a name, matched without regard to case in items, the project's list read
// for the call (fresh, never the cache of Tab). One item of the name — its
// id; two — USAGE naming their ids; none — the value as it is: the handler
// refuses it with its own reason, as before D57 (CONFIG_INVALID «not in the
// project», D54's NOT_FOUND).
func refByName[T any](items []T, ref string, id, name func(T) string, kind refKind) (string, error) {
	if ref == "" || looksUUID(ref) {
		return ref, nil
	}
	var ids []string
	for _, it := range items {
		if id(it) == ref {
			return ref, nil
		}
	}
	for _, it := range items {
		if strings.EqualFold(name(it), ref) {
			ids = append(ids, id(it))
		}
	}
	switch len(ids) {
	case 0:
		return ref, nil
	case 1:
		return ids[0], nil
	}
	return "", ambiguousRef(kind, ref, ids)
}

func ambiguousRef(kind refKind, ref string, ids []string) error {
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("%d of %s named %s: %s; nothing was done", "%d объекта (%s) с именем %s: %s; ничего не сделано",
			len(ids), i18n.T(kind.en, kind.ru), quoteCloud(ref), strings.Join(ids, ", ")),
		i18n.Tf("Name it by its id (%s).", "Укажите её по id (%s).", kind.list))
}

// subnetRef is the subnet a subnet argument names, by id or by name (D57).
func subnetRef(ctx context.Context, eng *engine.Engine, ref string) (string, error) {
	if ref == "" || looksUUID(ref) {
		return ref, nil
	}
	subs, err := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID)
	if err != nil {
		// The handler reads the list too and says why it failed.
		return ref, nil
	}
	return refByName(subs, ref, func(s *provision.Subnet) string { return s.ID }, func(s *provision.Subnet) string { return s.Name }, refSubnet)
}

// securityGroupRef is the group a security-group argument names, by id or by
// name (D57).
func securityGroupRef(ctx context.Context, eng *engine.Engine, ref string) (string, error) {
	if ref == "" || looksUUID(ref) {
		return ref, nil
	}
	sgs, err := eng.Cloud.ListSecurityGroups(ctx, eng.Config.ProjectID)
	if err != nil {
		// As the value was before D57: the handler's.
		return ref, nil
	}
	return refByName(sgs, ref, func(g *provision.SecurityGroup) string { return g.ID }, func(g *provision.SecurityGroup) string { return g.Name }, refSecurityGroup)
}

type catalogEntry struct{ id, name string }

// matchingEntries is shared with list filters, whose published errors
// name the filter rather than the operation that consumes a reference.
func matchingEntries(entries []catalogEntry, ref string) []catalogEntry {
	var matches []catalogEntry
	for _, e := range entries {
		if (e.id != "" && strings.EqualFold(e.id, ref)) || (e.name != "" && strings.EqualFold(e.name, ref)) {
			matches = append(matches, e)
		}
	}
	return matches
}

type vpcReferenceKey struct{}

func resolvedVPC(ctx context.Context, id string) (catalogEntry, bool) {
	ref, ok := ctx.Value(vpcReferenceKey{}).(catalogEntry)
	return ref, ok && ref.id == id
}

// projectVPCEntries reads the complete project catalogue for name lookups
// in resource commands and list filters.
func (r *Registry) projectVPCEntries(ctx context.Context) ([]catalogEntry, error) {
	cli, err := r.vpcClient(context.WithValue(ctx, needsProject{}, true))
	if err != nil {
		return nil, err
	}
	rows, err := cli.ListVPCReferences(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]catalogEntry, 0, len(rows))
	for _, row := range rows {
		id := stringOf(row["id"])
		if !isUUID(id) {
			return nil, clierrors.New(clierrors.CodeAPIError, i18n.T("a project VPC has no valid UUID", "у VPC проекта нет корректного UUID"), "")
		}
		entries = append(entries, catalogEntry{id, stringOf(row["name"])})
	}
	return entries, nil
}

// vpcRef resolves a name only in the configured project.
func (r *Registry) vpcRef(ctx context.Context, ref string) (catalogEntry, error) {
	entries, err := r.projectVPCEntries(ctx)
	if err != nil {
		return catalogEntry{}, err
	}
	matches := matchingEntries(entries, ref)
	switch len(matches) {
	case 0:
		return catalogEntry{}, clierrors.New(clierrors.CodeUsage, i18n.T("no VPC in this project has the given name", "в этом проекте нет VPC с указанным именем"), i18n.T("See vpc_list for the project's VPCs.", "VPC проекта — в выводе vpc_list."))
	case 1:
		return matches[0], nil
	}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.id)
	}
	return catalogEntry{}, ambiguousRef(refVPC, ref, ids)
}

// nameRef is an argument of a tool that names a resource by id or by name
// (D57).
type nameRef struct {
	arg  string
	kind refKind
}

// takesName: the argument of the tool is in Decl.RefArgs.
func takesName(tool, arg string) bool {
	for _, ra := range decls[tool].RefArgs {
		if ra.arg == arg {
			return true
		}
	}
	return false
}

// resolveRefs is the handler with the names of refs swapped for their ids
// (Decl.RefArgs); a call that passes ids reads nothing more, and what is not a
// name of the list reaches the handler as it is.
func (r *Registry) resolveRefs(refs []nameRef, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		var eng *engine.Engine
		var resolved map[string]string
		for _, ra := range refs {
			v := stringArg(args, ra.arg)
			if v == "" || looksUUID(v) {
				continue
			}
			if eng == nil {
				var overrides map[string]any
				if p := stringArg(args, "project_id"); p != "" {
					overrides = map[string]any{"ProjectID": p}
				}
				var err error
				if eng, err = r.authedEngine(ctx, overrides); err != nil {
					// The handler builds the engine too and refuses alike.
					return next(ctx, args)
				}
			}
			resolve := subnetRef
			if ra.kind == refSecurityGroup {
				resolve = securityGroupRef
			}
			id, err := resolve(ctx, eng, v)
			if err != nil {
				return errResult(err), true
			}
			if resolved == nil {
				resolved = make(map[string]string)
			}
			resolved[ra.arg] = id
		}
		return callWithResolvedRefs(ctx, next, args, resolved)
	}
}

// resolveVPCArg canonicalizes the reference before consent, ownership and
// journal checks, so they all address the same resource as the operation.
func (r *Registry) resolveVPCArg(arg string, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		ref := stringArg(args, arg)
		if ref == "" || isUUID(ref) {
			return next(ctx, args)
		}
		vpc, err := r.vpcRef(ctx, ref)
		if err != nil {
			return errResult(err), true
		}
		ctx = context.WithValue(ctx, vpcReferenceKey{}, vpc)
		return callWithResolvedRefs(ctx, next, args, map[string]string{arg: vpc.id})
	}
}

// callWithResolvedRefs passes canonical IDs to the next layer of this same
// tool invocation. The caller keeps its original arguments; no follow-up
// tool or next step receives this call's consent.
func callWithResolvedRefs(ctx context.Context, next Handler, args map[string]any, ids map[string]string) (string, bool) {
	if len(ids) == 0 {
		return next(ctx, args)
	}
	call := make(map[string]any, len(args))
	for key, value := range args {
		if id, ok := ids[key]; ok {
			value = id
		}
		call[key] = value
	}
	return next(ctx, call)
}
