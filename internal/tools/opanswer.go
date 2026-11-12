// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"regexp"
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
	"k8s_operation":   {{"k8s_get", "cluster_id"}, {"k8s_nodepool_get", "nodepool_id"}},
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
	addElapsed(m)
	s.addResourceStatus(ctx, tool, m)
	quotaFailure(tool, m)
	return toJSON(m), false
}

// quotaFailure names the cause of an operation that ended in an error for
// a quota, the way an answer of a refusal for a quota does (errAnswer,
// quotaRefusal): details.cause "quota", the platform's code and the same
// hint. The operation of a database create is taken, and seconds later
// ends with «acquire_quota failed» and the cluster FAILED, which the API
// does not delete (cloud-e2e 203 and 208): an agent that read only done and
// error repeated the create, and left one more cluster it cannot delete.
// The answer stays the success it was (release plan 1.9, variant A). The
// sign is a form of the closed list (quotaForm), not the word anywhere:
// the text carries names anyone with access to the project gives («Риск»,
// conclusion 161, У1). The text itself is not copied: it stays in error,
// the service's words (R-83).
func quotaFailure(tool string, m map[string]any) {
	if done, _ := m["done"].(bool); !done {
		return
	}
	msg, code, failed := opFailure(m["error"])
	text, _ := msg.(string)
	if !failed || !quotaForm.MatchString(text) {
		return
	}
	details := map[string]any{"cause": "quota"}
	if c := operationCode(code); c != nil {
		details["platform_code"] = c
	}
	m["details"] = details
	hint := quotaHint()
	if m["resource_status"] == "FAILED" {
		// What to do with it is the person's (conclusion 161, У3).
		if del := failedDeletes[tool]; del != "" {
			hint += " " + i18n.Tf("The resource is left FAILED. What to do with it is the person's decision: %s removes it after their approval; a database cluster in FAILED may not be deletable through the API — then the Cloud.ru console or support.",
				"Ресурс остался в FAILED. Что с ним делать — решает человек: %s удаляет его после его согласия; кластер базы в FAILED через API может не удалиться — тогда консоль Cloud.ru или поддержка.", del)
		} else {
			hint += " " + i18n.T("The resource is left FAILED. What to do with it is the person's decision.",
				"Ресурс остался в FAILED. Что с ним делать — решает человек.")
		}
	}
	m["hint"] = hint
}

// quotaForm are the forms of a refusal for a quota in the text of an
// operation's error, each a token of its own, not a part of a name
// (quota-db): acquire_quota (Managed PostgreSQL, cloud-e2e 203 and 208)
// and the forms of the codes of the specifications. A form not here is not
// told apart until a run shows it.
var quotaForm = regexp.MustCompile(`(?i)(^|[^a-z0-9_-])(acquire_quota|quota_exceeded|quota exceeded|quotaexceeded)([^a-z0-9_-]|$)`)

// failedDeletes are the deletes of the resource of an operation that is
// left FAILED, named in the hint: the database clusters, whose create a
// quota fails.
var failedDeletes = map[string]string{
	"pg_operation":    "pg_delete",
	"kafka_operation": "kafka_delete",
	"redis_operation": "redis_delete",
}

// operationCode is the code of a failed operation as a machine field: a
// number, or a token (letters, digits, underscores, 64 at most); a code
// that is a sentence, or long, is not copied («Риск», conclusion 147).
func operationCode(code any) any {
	switch c := code.(type) {
	case float64:
		return c
	case string:
		if operationToken.MatchString(c) {
			return c
		}
	}
	return nil
}

var operationToken = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// addElapsed puts into the answer m of an operation how long it has run,
// in whole seconds (elapsed_sec): from its creation to its last update once
// done, to now while it runs. The services name the times
// created_at/updated_at (DBaaS, NLB) or createdAt/updatedAt (VPC, DNS, AR,
// Certificate Manager). Without the time of its creation, or done without
// the time of its end, there is no elapsed_sec but a note of why: a done
// operation counted to now went on «running» at every read (FB160-4).
func addElapsed(m map[string]any) {
	created, ok := operationTime(m, "created_at", "createdAt")
	if !ok {
		m["elapsed_note"] = i18n.T("the operation carries no time of its creation", "операция не несёт времени создания")
		return
	}
	end := clock.Now()
	if done, _ := m["done"].(bool); done {
		updated, ok := operationTime(m, "updated_at", "updatedAt")
		if !ok {
			m["elapsed_note"] = i18n.T("the operation is done and carries no time of its end", "операция завершена и не несёт времени окончания")
			return
		}
		end = updated
	}
	sec := 0
	if !end.Before(created) {
		sec = int(end.Sub(created) / time.Second)
	}
	m["elapsed_sec"] = sec
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
