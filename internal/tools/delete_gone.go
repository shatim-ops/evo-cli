// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// A repeated delete leads to the same result as the first one (owner's
// decision 22.10, D26): a resource the cloud does not have is answered as
// deleted — exit 0, status already_deleted, existed false — in the preview
// as after consent, with no question asked. Three things stay errors: an
// id of the wrong form (USAGE, before the cloud), a parent that is not
// there (NOT_FOUND: the call names the wrong place rather than repeats
// itself), and a resource outside --allow-project, which the bound
// registry refuses as NOT_FOUND before the handler (H9: another project's
// and a gone resource read alike).

// deleteTarget is what a delete tool deletes: the argument naming it, the
// argument naming the resource it lives in, and its kind for the answer.
type deleteTarget struct {
	arg    string // names the resource
	parent string // names the resource it lives in; "" for none
	// uuid: the API names the resource by UUID (format uuid in its spec):
	// another form is USAGE before the cloud.
	uuid       bool
	what, whRU string
}

// deleteTargets are the tools that delete a resource
// (TestAlreadyDeleted_EveryDeleteDeclared keeps the list closed).
var deleteTargets = map[string]deleteTarget{
	"ar_delete":           {arg: "id", what: "artifact registry", whRU: "реестр артефактов"},
	"ca_delete":           {arg: "name", what: "container app", whRU: "приложение Container Apps"},
	"cert_delete":         {arg: "id", what: "certificate", whRU: "сертификат"},
	"destroy":             {arg: "vm_id", uuid: true, what: "VM", whRU: "ВМ"},
	"disk_delete":         {arg: "disk_id", uuid: true, what: "disk", whRU: "диск"},
	"dns_record_delete":   {arg: "record_id", what: "DNS record", whRU: "DNS-запись"},
	"dns_zone_delete":     {arg: "id", what: "DNS zone", whRU: "DNS-зона"},
	"image_delete":        {arg: "image_id", uuid: true, what: "image", whRU: "образ"},
	"k8s_delete":          {arg: "cluster_id", what: "Kubernetes cluster", whRU: "кластер Kubernetes"},
	"kafka_delete":        {arg: "cluster_id", what: "Kafka cluster", whRU: "кластер Kafka"},
	"lb_delete":           {arg: "id", what: "load balancer", whRU: "балансировщик"},
	"pg_db_delete":        {arg: "name", parent: "cluster_id", what: "database", whRU: "база данных"},
	"pg_delete":           {arg: "id", what: "PostgreSQL cluster", whRU: "кластер PostgreSQL"},
	"pg_user_delete":      {arg: "name", parent: "cluster_id", what: "database user", whRU: "пользователь базы данных"},
	"redis_delete":        {arg: "cluster_id", what: "Redis cluster", whRU: "кластер Redis"},
	"release_floating_ip": {arg: "id", uuid: true, what: "floating IP", whRU: "floating IP"},
	"s3_bucket_delete":    {arg: "bucket", what: "bucket", whRU: "бакет"},
	"s3_rm":               {arg: "key", parent: "bucket", what: "object", whRU: "объект"},
	"secret_delete":       {arg: "path", what: "secret", whRU: "секрет"},
	"snat_delete":         {arg: "id", uuid: true, what: "SNAT gateway", whRU: "SNAT-шлюз"},
	"tg_delete":           {arg: "id", what: "target group", whRU: "группа целей"},
	"vpc_delete":          {arg: "id", what: "VPC", whRU: "VPC"},
	"vpc_route_delete":    {arg: "route_id", parent: "vpc_id", what: "route", whRU: "маршрут"},
	// stack_destroy deletes the resources of a job through these tools and
	// takes their already_deleted as gone; the job itself is never deleted
	// (it is marked destroyed), so a job that is not there is a wrong id.
}

// isGone reports whether err says the resource is not there: its reason,
// as the error answer gives it, is NOT_FOUND or VM_NOT_FOUND.
func isGone(err error) bool {
	switch reason, _, _ := errReason(err, nil); reason {
	case clierrors.CodeNotFound, clierrors.CodeVMNotFound:
		return true
	}
	return false
}

// alreadyDeleted is the answer of a delete tool to a call whose resource ref
// the cloud does not have (err says so, isGone); key names ref in the
// answer ("" for the tool's target argument). ok is false for any other
// error, which stays the tool's error. A parent that is not there is not
// asked here: the handler answers it as the error it is.
func alreadyDeleted(tool, key, ref string, err error) (string, bool) {
	if err == nil || !isGone(err) {
		return "", false
	}
	t, ok := deleteTargets[tool]
	if !ok {
		panic("tools: alreadyDeleted for " + tool + ", which deleteTargets does not declare")
	}
	if key == "" {
		key = t.arg
	}
	return toJSON(map[string]any{
		"status":  "already_deleted",
		"existed": false,
		key:       ref,
		"note": i18n.Tf("no %s %s: it was deleted earlier, or the %s is wrong — check it in the list before relying on it",
			"%s %s отсутствует: удаление уже выполнено раньше или %s указан неверно — проверьте по списку, прежде чем на это полагаться",
			i18n.T(t.what, t.whRU), ref, key),
	}), true
}

// deleteFailed answers a delete tool's call that failed on its resource:
// already_deleted when the cloud does not have it, else the error.
func deleteFailed(tool, ref string, err error) (string, bool) {
	return deleteFailedAs(tool, "", ref, err)
}

// deleteFailedAs is deleteFailed for a resource named by another argument
// than the tool's target one (key).
func deleteFailedAs(tool, key, ref string, err error) (string, bool) {
	if out, ok := alreadyDeleted(tool, key, ref, err); ok {
		return out, false
	}
	return errResult(err), true
}

// checkedDelete refuses, before the handler, an id of a delete tool that
// is not of its form: a UUID the API's spec declares (D26: a wrong form is
// USAGE, not "already deleted").
func checkedDelete(name string, next Handler) Handler {
	t, ok := deleteTargets[name]
	if !ok || !t.uuid {
		return next
	}
	return func(ctx context.Context, args map[string]any) (string, bool) {
		// A blank value is the handler's own "which argument" error.
		if v := stringArg(args, t.arg); strings.TrimSpace(v) != "" && !isUUID(v) {
			return errResult(clierrors.New(clierrors.CodeUsage,
				i18n.Tf("%s %q is not an id: a %s is named by a UUID", "%s %q — не id: %s задаётся UUID", t.arg, v, i18n.T(t.what, t.whRU)),
				i18n.T("Take the id from the list of the project's resources.", "id — в списке ресурсов проекта."))), true
		}
		return next(ctx, args)
	}
}
