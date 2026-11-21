// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
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

// WithNetworkSession carries the validated HTTP session for recovery guidance.
func WithNetworkSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, networkSessionKey{}, session)
}

// HTTP dispatch always sets the session context, including for stateless requests.
func statelessNetworkHTTP(ctx context.Context) bool {
	session, http := ctx.Value(networkSessionKey{}).(string)
	return http && session == ""
}

// Do not infer a negative outcome from a list. Only a matching, identified
// object can settle an unknown create. Return the occupied-target answer of
// the same preview, even if a later read no longer sees the object.
func (r *Registry) settleNetworkConflict(ctx context.Context, mine []intent.Intent, answer string) {
	if r.readOnly {
		return
	}
	var result struct {
		ID string `json:"existing_id"`
	}
	if json.Unmarshal([]byte(answer), &result) != nil || result.ID == "" {
		return
	}
	for _, in := range mine {
		if in.ProjectID == "" || in.Key == nil || in.Key.KeyID == "" {
			continue
		}
		if _, found, id := r.checkByList(ctx, in, in.ProjectID); found && id == result.ID {
			intent.Close(in, settledOutcome(in, id))
			forgetPending(ctx)
		}
	}
}
