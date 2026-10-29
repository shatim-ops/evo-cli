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
	return "", clierrors.New(clierrors.CodeUsage,
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

// nameRef is an argument of a tool that names a resource by id or by name
// (D57).
type nameRef struct {
	arg  string
	kind refKind
}

// refArgs are the reference arguments a name may stand in for (D57): the
// registry swaps the name for its id before the handler, so the checks, the
// preview and the question see the id, and a name the project lacks is
// refused before them.
var refArgs = map[string][]nameRef{
	"provision":        {{"subnet_id", refSubnet}},
	"pg_create":        {{"subnet_id", refSubnet}},
	"kafka_create":     {{"subnet_id", refSubnet}},
	"redis_create":     {{"subnet_id", refSubnet}},
	"k8s_create":       {{"subnet_id", refSubnet}},
	"k8s_plan":         {{"subnet_id", refSubnet}},
	"lb_create":        {{"subnet_id", refSubnet}},
	"k8s_add_nodepool": {{"nodes_subnet_id", refSubnet}, {"security_group_id", refSecurityGroup}},
}

// takesName: the argument of the tool is in refArgs.
func takesName(tool, arg string) bool {
	for _, ra := range refArgs[tool] {
		if ra.arg == arg {
			return true
		}
	}
	return false
}

// resolveRefs is the handler with the names of refs swapped for their ids
// (refArgs); a call that passes ids reads nothing more, and what is not a
// name of the list reaches the handler as it is.
func (r *Registry) resolveRefs(refs []nameRef, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		var eng *engine.Engine
		var out map[string]any
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
			if out == nil {
				out = make(map[string]any, len(args))
				for k, a := range args {
					out[k] = a
				}
			}
			out[ra.arg] = id
		}
		if out == nil {
			return next(ctx, args)
		}
		return next(ctx, out)
	}
}
