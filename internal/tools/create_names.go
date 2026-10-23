// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The name of a new resource has one rule (owner's decision 23.10, D30): a
// create whose service states a rule checks that one in its handler; every
// other create gets plainName before its handler — a name blank once
// trimmed, with a space at either end, a control character, or over 255
// characters is USAGE before the cloud, confirmed or not. "web " passed by
// a taken "web" and made a second resource (Cursor 1.2.3: lb_create; the
// creates without a rule of their own alike).
// TestCreateNames_EveryCreateDeclared keeps the list closed.

// createName is what names the resource a create makes.
type createName struct {
	args []string // the arguments that name the new resource
	// rule is the service's own rule for the name, which the handler
	// checks before the cloud (stricter than plainName); "" for
	// plainName, which the registry checks before the handler.
	rule       string
	what, whRU string
}

// createNames are the creates and the rule of the name of what they make.
var createNames = map[string]createName{
	"ar_create":         {args: []string{"name"}, what: "registry", whRU: "реестр"},
	"ca_deploy":         {args: []string{"name"}, rule: "caNameRe: an RFC 1123 label"},
	"cert_import":       {args: []string{"name"}, what: "certificate", whRU: "сертификат"},
	"cert_issue_le":     {args: []string{"name"}, what: "certificate", whRU: "сертификат"},
	"disk_create":       {args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"},
	"dns_record_create": {args: []string{"name"}, what: "DNS record", whRU: "DNS-запись"},
	"dns_zone_create":   {args: []string{"name", "domain"}, what: "DNS zone", whRU: "DNS-зона"},
	"image_create":      {args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"},
	"k8s_add_nodepool":  {args: []string{"name"}, what: "node pool", whRU: "группа узлов"},
	"k8s_create":        {args: []string{"name"}, rule: "k8sNameRe: the mk8s specification"},
	"kafka_create":      {args: []string{"name"}, what: "Kafka cluster", whRU: "кластер Kafka"},
	"lb_create":         {args: []string{"name"}, what: "load balancer", whRU: "балансировщик"},
	"pg_backup_create":  {args: []string{"name"}, what: "backup", whRU: "резервная копия"},
	"pg_create":         {args: []string{"name"}, what: "PostgreSQL cluster", whRU: "кластер PostgreSQL"},
	"pg_db_create":      {args: []string{"name"}, what: "database", whRU: "база данных"},
	"pg_user_create":    {args: []string{"name"}, what: "database user", whRU: "пользователь базы данных"},
	"provision":         {args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"},
	"redis_create":      {args: []string{"name"}, what: "Redis cluster", whRU: "кластер Redis"},
	"s3_bucket_create":  {args: []string{"bucket"}, rule: "bucketNameRe and the S3 client's rule"},
	"secret_create":     {args: []string{"path"}, what: "secret", whRU: "секрет"},
	"snat_create":       {args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"},
	"tg_create":         {args: []string{"name"}, what: "target group", whRU: "целевая группа"},
	"vpc_create":        {args: []string{"name"}, what: "VPC", whRU: "VPC"},
}

// namelessCreates make a resource whose name the call does not give, and
// why.
var namelessCreates = map[string]string{
	"deploy":             "the VM is named by the project (.evo.yaml, the directory), checked by provision.ValidName",
	"deploy_apply":       "the VM is named by the saved plan, checked by deploy_plan",
	"k8s_add_addon":      "the addon is named by the catalogue (k8s_addons)",
	"s3_put":             "an object key may hold spaces anywhere: S3 takes any UTF-8",
	"secret_add_version": "a version of a secret that is there",
	"vpc_route_create":   "a route is known by its destination, named by no argument",
}

// checkedCreateName refuses, before the handler, a name of a new resource
// that plainName refuses, for a create without a rule of its own (D30). An
// argument not given is the handler's: required, or a default name.
func checkedCreateName(name string, next Handler) Handler {
	c, ok := createNames[name]
	if !ok || c.rule != "" {
		return next
	}
	return func(ctx context.Context, args map[string]any) (string, bool) {
		for _, a := range c.args {
			if v, ok := args[a].(string); ok && v != "" {
				if err := plainName(i18n.T(c.what, c.whRU), a, v); err != nil {
					return errResult(err), true
				}
			}
		}
		return next(ctx, args)
	}
}
