// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// An operation says it is done or not; an agent waiting for a cluster wants
// to know how long it has run and what the resource is now (CLAUDE.md §6,
// axis 2; Ц-29, G130-28: k8s_create ran 34 minutes against 5–12 documented,
// and pg_operation said nothing but done: false). Every *_operation answers
// through operation: the operation as the service gives it, elapsed_sec
// from its own times and resource_status from the read of its resource.

// resourceRead is a tool that reads the resource of an operation and the
// argument it takes the resource's id in.
type resourceRead struct{ tool, arg string }

// operationReads are the reads of each *_operation's resource, tried in
// order: an NLB operation is of a balancer or of a target group, a DNS one
// of a zone or of a record (TestOperation_EveryOperationDeclared keeps the
// table closed).
var operationReads = map[string][]resourceRead{
	"ar_operation":    {{"ar_get", "id"}},
	"cert_operation":  {{"cert_get", "id"}},
	"dns_operation":   {{"dns_zone_get", "id"}, {"dns_record_get", "record_id"}},
	"kafka_operation": {{"kafka_get", "cluster_id"}},
	"lb_operation":    {{"lb_get", "id"}, {"tg_get", "id"}},
	"pg_operation":    {{"pg_get", "id"}},
	"redis_operation": {{"redis_get", "cluster_id"}},
	"vpc_operation":   {{"vpc_get", "id"}},
}

// operation answers a *_operation call: get reads the operation named by
// operation_id, wait_seconds waits for it to end (waitForOpJSON).
func (s *Registry) operation(ctx context.Context, tool string, args map[string]any, get func(context.Context) (any, error)) (string, bool) {
	fetch := func(ctx context.Context) (map[string]any, error) {
		op, err := get(ctx)
		if err != nil {
			return nil, err
		}
		return opToMap(op), nil
	}
	var m map[string]any
	var err error
	if w := pgInt(args, "wait_seconds", 0); w > 0 {
		m, err = waitForOpJSON(ctx, w, fetch)
	} else {
		m, err = fetch(ctx)
	}
	if err != nil {
		return errResult(err), true
	}
	if sec, ok := operationElapsed(m); ok {
		m["elapsed_sec"] = sec
	}
	s.addResourceStatus(ctx, tool, m)
	return toJSON(m), false
}

// operationElapsed is how long the operation m has run, in whole seconds:
// from its creation to its last update once done, to now while it runs.
// The services name the times created_at/updated_at (DBaaS, NLB) or
// createdAt/updatedAt (VPC, DNS, AR, Certificate Manager); all eight
// specifications carry them. ok is false when the answer has no creation
// time the clock can read.
func operationElapsed(m map[string]any) (int, bool) {
	created, ok := operationTime(m, "created_at", "createdAt")
	if !ok {
		return 0, false
	}
	end := clock.Now()
	if done, _ := m["done"].(bool); done {
		if updated, ok := operationTime(m, "updated_at", "updatedAt"); ok {
			end = updated
		}
	}
	if end.Before(created) {
		return 0, true
	}
	return int(end.Sub(created) / time.Second), true
}

func operationTime(m map[string]any, keys ...string) (time.Time, bool) {
	for _, k := range keys {
		if v, _ := m[k].(string); v != "" {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// addResourceStatus sets resource_status of the operation m of tool: the
// status the first read of operationReads that knows the resource gives,
// "not_found" with resource_status_note when none does (a delete that is
// done, or a resource of a kind the reads do not know), and
// resource_status_error with the read's reason when a read fails otherwise.
// An operation without a resource id gets neither.
func (s *Registry) addResourceStatus(ctx context.Context, tool string, m map[string]any) {
	id, _ := m["resource_id"].(string)
	if id == "" {
		id, _ = m["resourceId"].(string)
	}
	if id == "" {
		return
	}
	for _, r := range operationReads[tool] {
		res, err := s.callTool(ctx, r.tool, map[string]any{r.arg: id})
		if err != nil {
			reason, _ := res["reason"].(string)
			if reason == clierrors.CodeNotFound {
				continue
			}
			if reason == "" {
				reason = clierrors.CodeAPIError
			}
			m["resource_status_error"] = reason
			return
		}
		for _, k := range []string{"status", "state", "phase"} {
			if v, _ := res[k].(string); v != "" {
				m["resource_status"] = v
				return
			}
		}
		return
	}
	// The reads know the kind of resource the tool's create gives; a route
	// of a VPC or a backup of a cluster is not one of them.
	reads := make([]string, 0, len(operationReads[tool]))
	for _, r := range operationReads[tool] {
		reads = append(reads, r.tool)
	}
	m["resource_status"] = "not_found"
	m["resource_status_note"] = i18n.Tf("%s does not find %s: it is deleted, or the operation is of a resource %s does not read (a route, a backup)",
		"%s не находит %s: ресурс удалён или операция — над ресурсом, который %s не читает (маршрут, резервная копия)",
		strings.Join(reads, ", "), id, strings.Join(reads, ", "))
}
