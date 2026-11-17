// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// An operation says it is done or not; an agent waiting for a cluster wants
// to know how long it has run and what the resource is now (CLAUDE.md §6,
// axis 2; Ц-29, G130-28: k8s_create ran 34 minutes against 5–12 documented,
// and pg_operation said nothing but done: false). Every *_operation answers
// through operation: the operation as the service gives it, elapsed_sec
// from its own times and resource_status from the read of its resource.

// resourceRead is a tool that reads the resource of an operation and the
// argument it takes the resource's id in. noState, when set, says why its
// answer names no state: the service's specification gives the resource
// none (a VPC, a DNS record) — the read that finds it then says so in a
// note, not as an error (Астра, #1953).
type resourceRead struct{ tool, arg, noState, noStateRU string }

// operationReads are the reads of each *_operation's resource, tried in
// order: an NLB operation is of a balancer or of a target group, a DNS one
// of a zone or of a record (TestOperation_EveryOperationDeclared keeps the
// table closed).
var operationReads = map[string][]resourceRead{
	"ar_operation":    {{"ar_get", "id", "", ""}},
	"cert_operation":  {{"cert_get", "id", "the state of a certificate is the object issueState, not a word of the answer", "состояние сертификата — объект issueState, а не слово ответа"}},
	"dns_operation":   {{"dns_zone_get", "id", "", ""}, {"dns_record_get", "record_id", "a DNS record has no state in the DNS specification", "у записи DNS в спецификации DNS нет состояния"}},
	"k8s_operation":   {{"k8s_get", "cluster_id", "", ""}, {"k8s_nodepool_get", "nodepool_id", "", ""}},
	"kafka_operation": {{"kafka_get", "cluster_id", "", ""}},
	"lb_operation":    {{"lb_get", "id", "", ""}, {"tg_get", "id", "", ""}},
	"pg_operation":    {{"pg_get", "id", "", ""}},
	"redis_operation": {{"redis_get", "cluster_id", "", ""}},
	"vpc_operation":   {{"vpc_get", "id", "a VPC has no state in the VPC specification (only its static routes do)", "у сети VPC в спецификации VPC нет состояния (оно есть только у статических маршрутов)"}},
}

// operation answers a *_operation call: get reads the operation named by
// operation_id, wait_seconds waits for it to end (waitForOpJSON).
func (s *Registry) operation(ctx context.Context, tool string, args map[string]any, get func(context.Context) (any, error)) (string, bool) {
	fetch := func(ctx context.Context) (map[string]any, error) {
		op, err := get(ctx)
		if err != nil {
			return nil, err
		}
		m := opToMap(op)
		// The fields the tool declares evo's own (Decl.Untrusted.own) are
		// written by evo below; a service's key of the same name is not
		// passed on as evo's word («Риск» 293, У1).
		for _, k := range decls[tool].Untrusted.own {
			delete(m, k)
		}
		return m, nil
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
	if !failed || !isQuotaForm(text) {
		return
	}
	details := map[string]any{"cause": "quota"}
	if c := operationCode(code); c != nil {
		details["platform_code"] = c
	}
	m["details"] = details
	hint := quotaHint(true)
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
// and the forms of the codes of the specifications — a code that ends in
// quota_exceeded, as quotaCode reads one of a call (organization_quota_exceeded,
// «Риск», #1522, Р1). A form not here is not told apart until a run shows it.
var quotaForm = regexp.MustCompile(`(?i)(^|[^a-z0-9_-])(acquire_quota|[a-z0-9_]{0,50}quota_exceeded|quota exceeded|quotaexceeded)([^a-z0-9_-]|$)`)

// quotedPart is a part of a text in quotes — "…", '…', `…`, «…»: a name
// anyone with access to the project gives, which may be a form of
// quotaForm itself (FB19I-28).
var quotedPart = regexp.MustCompile("\"[^\"]*\"|'[^']*'|`[^`]*`|«[^»]*»")

// fileSystemQuota is the quota of a file system (EDQUOT, «Disk quota
// exceeded»), which a service may pass on from a disk: not a quota of the
// platform that support raises (FB19I-28).
var fileSystemQuota = regexp.MustCompile(`(?i)\bdisk quota exceeded\b`)

// isQuotaForm reports a form of quotaForm in text outside its parts in
// quotes and the quota of a file system.
func isQuotaForm(text string) bool {
	text = quotedPart.ReplaceAllString(text, " ")
	text = fileSystemQuota.ReplaceAllString(text, " ")
	return quotaForm.MatchString(text)
}

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

// goingOn names, in the answer ans of a change whose operation opID the
// cloud is still making, the machine's next step — the read of the
// operation, opTool, whose answer ends with the state of the resource —
// and the state of the resource now, which the agent would otherwise read
// blindly from a list (CLAUDE.md §6, axis 2; Астра, #1815). The state is
// the read of opTool's resource id (addResourceStatus), or, for a resource
// with no read of its own (a backup, an addon), listed — the read of the
// list that names it. One place for every create that answers an
// operation.
//
// The write is made and paid for: the read of the state is one attempt
// within goingOnLimit for all of its reads together (k8s_add_nodepool's
// are two), its failure named in resource_status_error, not waited out —
// the answer with the id goes («Риск», conclusion 242, У1; as vm_get's
// read of the interfaces, conclusion 209). A resource the reads do not
// find yet may be on its way — not "deleted", as a done operation's read
// says — and the answer says not to create it again (У2).
func (s *Registry) goingOn(ctx context.Context, ans map[string]any, opTool, opID, id string, listed func(context.Context) (map[string]any, error)) {
	if opID == "" {
		return
	}
	nextStep(ans, opTool, map[string]any{"operation_id": opID, "wait_seconds": opWaitMaxSeconds})
	ctx, cancel := context.WithTimeout(httpretry.Once(ctx), goingOnLimit)
	defer cancel()
	m := map[string]any{"resource_id": id}
	switch {
	case listed != nil:
		item, err := listed(ctx)
		switch {
		case err != nil:
			m["resource_status_error"] = clierrors.Reason(err)
		case item == nil:
			m["resource_status"] = "not_found"
			m["resource_status_note"] = notFoundYet()
		default:
			for _, k := range []string{"status", "state", "phase"} {
				if v, _ := item[k].(string); v != "" {
					m["resource_status"] = v
					cloudText(m, "resource_status")
					break
				}
			}
		}
	case id != "":
		s.addResourceStatus(ctx, opTool, m)
		switch note, _ := m["resource_status_note"].(string); {
		case m["resource_status"] == "not_found":
			m["resource_status_note"] = notFoundYet()
		case note != "":
			// A create's answer: the step is the read, not a repeat.
			m["resource_status_note"] = note + i18n.T(" — do not create it again", " — не создавайте его повторно")
		}
	}
	// A state evo cannot name is said, never left out (Астра, #1924): the
	// list names the resource with no state, or the operation names no
	// resource.
	if m["resource_status"] == nil && m["resource_status_error"] == nil && m["resource_status_note"] == nil {
		m["resource_status_error"] = clierrors.CodeAPIError
		m["resource_status_note"] = noStateRead(id)
	}
	for _, k := range []string{"resource_status", "resource_status_error", "resource_status_note"} {
		if v, ok := m[k]; ok {
			ans[k] = v
		}
	}
	if m["_untrusted"] != nil {
		cloudText(ans, "resource_status")
	}
}

// goingOnLimit bounds the read of the state goingOn adds to the answer of
// a create: the limit of a preview's read.
var goingOnLimit = engine.PreviewLookupTimeout

// notFoundYet is what "not_found" means right after a create.
func notFoundYet() string {
	return i18n.T("the reads do not find it yet: it may still be on its way; the read of the operation says when it ends and how — do not create it again",
		"чтение его ещё не находит: он может быть ещё в пути; чтение операции скажет, когда и чем она кончится, — не создавайте его повторно")
}

// noStateRead is resource_status_note when the create's answer cannot
// name the state of resource id: the read found it with no state, or the
// cloud's answer named no resource ("").
func noStateRead(id string) string {
	if id == "" {
		return i18n.T("the cloud's answer to the create names no id of the resource, so its state is not read; the read of the operation says when it ends and what it made — do not create it again",
			"ответ облака на создание не назвал id ресурса, и его состояние не прочитано; чтение операции скажет, когда она кончится и что создала, — не создавайте его повторно")
	}
	return i18n.T("the read finds the resource with no state; the read of the operation says when it ends and how — do not create it again",
		"чтение находит ресурс без состояния; чтение операции скажет, когда и чем она кончится, — не создавайте его повторно")
}

// cloudText adds fields to the "_untrusted" of answer m: their text is the
// service's, not evo's — a state anyone with access to the project may have
// steered, a value of a catalogue (Астра, #1925). One point for the fields
// evo copies from a read into its own answer; markUntrusted keeps them
// beside those the tool declares.
func cloudText(m map[string]any, fields ...string) {
	var has []string
	switch u := m["_untrusted"].(type) {
	case []string:
		has = u
	case []any:
		has = stringList(u)
	}
	for _, f := range fields {
		if !slices.Contains(has, f) {
			has = append(has, f)
		}
	}
	m["_untrusted"] = has
}

// listedBy is the read of a resource with no read of its own: the item of
// list whose id is id or, without one, whose name is name; nil when none.
func listedBy(list func(context.Context) ([]map[string]any, error), id, name string) func(context.Context) (map[string]any, error) {
	return func(ctx context.Context) (map[string]any, error) {
		items, err := list(ctx)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if id != "" && it["id"] == id || id == "" && name != "" && it["name"] == name {
				return it, nil
			}
		}
		return nil, nil
	}
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
				cloudText(m, "resource_status")
				return
			}
		}
		// Found, with no state: said, never left out (Астра, #1953).
		if r.noState != "" {
			m["resource_status_note"] = i18n.T(fmt.Sprintf("%s finds the resource; it has no state to name: %s", r.tool, r.noState),
				fmt.Sprintf("%s находит ресурс; состояния у него нет: %s", r.tool, r.noStateRU))
			return
		}
		m["resource_status_error"] = clierrors.CodeAPIError
		m["resource_status_note"] = i18n.Tf("%s finds the resource with no state: what it is now is not known; the read of the operation says when it ends and how",
			"%s находит ресурс без состояния: каков он сейчас, неизвестно; чтение операции скажет, когда и чем она кончится", r.tool)
		return
	}
	// The reads know the kind of resource the tool's create gives; a route
	// of a VPC or a backup of a cluster is not one of them.
	reads := make([]string, 0, len(operationReads[tool]))
	for _, r := range operationReads[tool] {
		reads = append(reads, r.tool)
	}
	m["resource_status"] = "not_found"
	// While the operation goes on, a resource not found may be one on its
	// way (a create, a repeat of it, D113): the note says what the read
	// proved, not that it is deleted («Риск», conclusion 310, 310-1).
	if done, _ := m["done"].(bool); !done {
		m["resource_status_note"] = i18n.Tf("%s does not find %s yet: while the operation goes on, its resource may not be shown yet; the read of the operation says when it ends",
			"%s пока не находит %s: пока операция идёт, её ресурс может быть ещё не виден; чтение операции скажет, когда она кончится",
			strings.Join(reads, ", "), quoteCloud(id))
		return
	}
	// Done: a create's resource may still be not shown yet (312-1).
	m["resource_status_note"] = i18n.Tf("%s does not find %s: it is not shown yet, or deleted, or the operation is of a resource %s does not read (a route, a backup)",
		"%s не находит %s: ресурс ещё не виден, удалён или операция — над ресурсом, который %s не читает (маршрут, резервная копия)",
		strings.Join(reads, ", "), quoteCloud(id), strings.Join(reads, ", "))
}
