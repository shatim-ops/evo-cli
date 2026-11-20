// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func dnsRecordIntent(ctx context.Context, r *Registry, args map[string]any) (string, string, error) {
	name := nameArg(args)
	recordType := strings.ToLower(stringArg(args, "record_type"))
	id := stringArg(args, "zone_id")
	if name == "" || recordType == "" || id == "" {
		return name, "", nil
	}
	vis := dnsVisibility(args)
	if stringArg(args, "type") == "" {
		cli, err := r.dnsClient(ctx)
		if err != nil {
			return "", "", err
		}
		vis, err = dnsZoneVisibility(ctx, cli, args, id)
		if err != nil {
			return "", "", err
		}
	}
	return name, vis + "_" + recordType + "_zone_id=" + id, nil
}

func snatIntent(ctx context.Context, r *Registry, args map[string]any) (string, string, error) {
	name := nameArg(args)
	if name == "" {
		return "", "", nil
	}
	if err := provision.ValidName(i18n.T("SNAT gateway", "SNAT-шлюза"), name); err != nil {
		return "", "", err
	}
	id, _, err := r.resolveZone(ctx, args)
	return name, "availability_zone_id=" + id, err
}

// Before 1.10 DNS stored just the label and a route just its VPC.
// An ambiguous older record can block a send but cannot prove success.
func networkIntentConflicts(in intent.Intent, project, name, target string, args map[string]any) bool {
	if in.ProjectID != project {
		if in.ProjectID != "" || (in.Tool != "vpc_route_create" && in.Tool != "dns_record_create") {
			return false
		}
	}
	switch in.Tool {
	case "snat_create":
		return in.Name == name || (in.Target != "" && in.Target == target)
	case "dns_record_create":
		if in.Target == target {
			return strings.EqualFold(in.Name, name)
		}
		if strings.HasPrefix(in.Target, "zone_id=") {
			return in.Target == "zone_id="+stringArg(args, "zone_id") && strings.EqualFold(in.Name, nameArg(args))
		}
	case "vpc_route_create":
		if in.Name == "" {
			return in.Target == target
		}
	}
	return in.Target == target && in.Name == name
}

// One lock file per registered operation, independent of user input.
// SNAT conflicts on either name or zone, so a lock on their pair would
// let two conflicting calls check an empty directory at the same time.
func (r *Registry) lockNetworkIntents(tool string) (func(), error) {
	root, _ := r.stateDir()
	unlock, err := state.FileLock(filepath.Join(config.DataDir(root), tool+".lock"), 5*time.Second)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeStateLocked, i18n.T("could not lock network create intents", "не удалось заблокировать записи сетевых созданий"), i18n.T("Check the state directory and retry.", "Проверьте каталог состояния и повторите вызов."), err)
	}
	return unlock, nil
}

type networkSessionKey struct{}

// WithNetworkSession separates the questions delivered to distinct HTTP
// sessions of a shared MCP registry. Stdio has its own registry.
func WithNetworkSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, networkSessionKey{}, session)
}

// networkQuestion remembers which question this client saw. The shared
// intent stays until an answering call; a CLI preview, read-only server
// or another MCP registry cannot release it for a script with --yes.
// At most one question per network tool is cached. A displaced question
// asks again. A restart likewise asks again.
func (r *Registry) networkQuestion(ctx context.Context, tool string, args map[string]any, mine []intent.Intent, confirmed bool) bool {
	if callerOf(ctx) != nil || r.readOnly {
		return false
	}
	ids := make([]string, 0, len(mine))
	for _, in := range mine {
		ids = append(ids, in.ID)
	}
	slices.Sort(ids)
	scope, _ := ctx.Value(networkSessionKey{}).(string)
	question := madeKey(tool, r.canonArgs(tool, args), strings.Join(ids, ":"), scope)
	r.networkQuestionMu.Lock()
	defer r.networkQuestionMu.Unlock()
	if confirmed && r.networkQuestions[tool] == question {
		delete(r.networkQuestions, tool)
		return true
	}
	if r.networkQuestions == nil {
		r.networkQuestions = make(map[string]string)
	}
	r.networkQuestions[tool] = question
	return false
}
