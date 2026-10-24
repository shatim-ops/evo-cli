// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
)

// What a create made is named in one place (G13-14, Grok wave 2): where its
// answer gives the id, how to find it by name, and what deletes it. A
// synchronous stack step took the id from vm_id, id or path of the answer
// and wrote "ok" when none was there; snat_create answers nat_gateway.id,
// so stack_destroy got "ok" and the billed gateway stayed.
// TestCreated_EveryCreateDeclared keeps the list closed, and the fault
// matrix checks every id against what the call made (invariant c).
//
// For now lostCreate still takes the id field and the list of its own
// arguments (two sources); the next change makes it read them from here.

// created is what names the resource a create makes. One of id, arg and
// none is set.
type created struct {
	// id are the fields of the answer that hold the id the delete takes,
	// "a.b" for field b of object a: that of the create's answer, then
	// that of lostCreate's answer when the create's own was lost.
	id []string
	// arg is the create's argument the delete takes instead of an id: a
	// secret is deleted by its path, an object by its key.
	arg string
	// none says why the create makes nothing a delete names.
	none string
	// list and field are the list tool and its array of resources, to find
	// the one named as the create's name argument (createNames) when the
	// answer did not say; listArgs are the create's arguments the list
	// takes too. Declared for the creates of stack steps.
	list, field string
	listArgs    []string
	// delete is the tool that deletes it (deleteTargets); "" for none.
	delete string
}

// createdIDs are the creates and what names what they make.
var createdIDs = map[string]created{
	"ar_create":         {id: []string{"registry_id"}, list: "ar_list", field: "registries", delete: "ar_delete"},
	"ca_deploy":         {arg: "name", delete: "ca_delete"},
	"cert_import":       {id: []string{"certificate.id", "certificate_id"}, delete: "cert_delete"},
	"cert_issue_le":     {id: []string{"certificate_id"}, delete: "cert_delete"},
	"deploy":            {id: []string{"vm_id"}, delete: "destroy"},
	"deploy_apply":      {id: []string{"vm_id"}, delete: "destroy"},
	"disk_create":       {id: []string{"disk.id", "disk_id"}, delete: "disk_delete"},
	"dns_record_create": {id: []string{"record_id"}, list: "dns_records", field: "records", listArgs: []string{"zone_id"}, delete: "dns_record_delete"},
	"dns_zone_create":   {id: []string{"zone_id"}, delete: "dns_zone_delete"},
	"image_create":      {id: []string{"image.id", "image_id"}, delete: "image_delete"},
	"k8s_add_addon":     {none: "an addon of a cluster, known by its name; it goes with the cluster"},
	"k8s_add_nodepool":  {id: []string{"nodepool_id"}},
	"k8s_create":        {id: []string{"cluster_id"}, list: "k8s_list", field: "clusters", delete: "k8s_delete"},
	"kafka_create":      {id: []string{"cluster_id"}, list: "kafka_list", field: "clusters", delete: "kafka_delete"},
	"lb_create":         {id: []string{"lb_id"}, list: "lb_list", field: "load_balancers", delete: "lb_delete"},
	"pg_backup_create":  {id: []string{"backup_id"}},
	"pg_create":         {id: []string{"cluster_id"}, list: "pg_list", field: "clusters", delete: "pg_delete"},
	"pg_db_create":      {arg: "name", delete: "pg_db_delete"},
	"pg_user_create":    {arg: "name", delete: "pg_user_delete"},
	"provision":         {id: []string{"vm_id"}, list: "vm_list", field: "vms", delete: "destroy"},
	"redis_create":      {id: []string{"cluster_id"}, list: "redis_list", field: "clusters", delete: "redis_delete"},
	"s3_bucket_create":  {arg: "bucket", delete: "s3_bucket_delete"},
	"s3_put":            {arg: "key", delete: "s3_rm"},
	"secret_create":     {arg: "path", delete: "secret_delete"},
	"snat_create":       {id: []string{"nat_gateway.id", "nat_gateway_id"}, list: "snat_list", field: "nat_gateways", delete: "snat_delete"},
	"tg_create":         {id: []string{"tg_id"}, list: "tg_list", field: "target_groups", delete: "tg_delete"},
	"vpc_create":        {id: []string{"vpc_id"}, list: "vpc_list", field: "vpcs", delete: "vpc_delete"},
	"vpc_route_create":  {id: []string{"route_id"}, delete: "vpc_route_delete"},
}

// createdID is the id of what tool made, as its answer res gives it; ""
// when the answer names none (an error, or a create with arg or none).
func createdID(tool string, res map[string]any) string {
	for _, path := range createdIDs[tool].id {
		v := any(res)
		for _, k := range strings.Split(path, ".") {
			m, _ := v.(map[string]any)
			v = m[k]
		}
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

// createdRef is what the delete of tool takes to delete what a successful
// call with args made: the id its answer res gives, or the argument that
// names it.
func createdRef(tool string, args, res map[string]any) string {
	if a := createdIDs[tool].arg; a != "" {
		return stringArg(args, a)
	}
	return createdID(tool, res)
}
