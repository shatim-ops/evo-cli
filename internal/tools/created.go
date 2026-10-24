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
// so stack_destroy got "ok" and the billed gateway stayed. lostCreate
// reads the id field of its answer and the list tool here as well.

// created is what names the resource a create makes. One of id, arg and
// none is set.
type created struct {
	// id is the field of the create's answer that holds the id the delete
	// takes, "a.b" for field b of object a.
	id string
	// lost is the field of lostCreate's answer that holds the id, when the
	// create's own answer was lost; "" for id.
	lost string
	// arg is the create's argument the delete takes instead of an id: a
	// secret is deleted by its path, an object by its key.
	arg string
	// none says why the create makes nothing a delete names.
	none string
	// list and field are the list tool and its array of resources, to find
	// the one named as the create's name: by stack steps (createNames) and
	// in lostCreate's answer. listArgs are the create's arguments the list
	// takes too.
	list, field string
	listArgs    []string
	// delete is the tool that deletes it (deleteTargets); "" for none.
	delete string
}

// lostField is the field of lostCreate's answer that holds the id.
func (c created) lostField() string {
	if c.lost != "" {
		return c.lost
	}
	return c.id
}

// createdIDs are the creates and what names what they make.
var createdIDs = map[string]created{
	"ar_create":         {id: "registry_id", list: "ar_list", field: "registries", delete: "ar_delete"},
	"ca_deploy":         {arg: "name", lost: "id", list: "ca_list", field: "containers", delete: "ca_delete"},
	"cert_import":       {id: "certificate.id", lost: "certificate_id", list: "cert_list", field: "certificates", delete: "cert_delete"},
	"cert_issue_le":     {id: "certificate_id", list: "cert_list", field: "certificates", delete: "cert_delete"},
	"deploy":            {id: "vm_id", delete: "destroy"},
	"deploy_apply":      {id: "vm_id", delete: "destroy"},
	"disk_create":       {id: "disk.id", lost: "disk_id", list: "disk_list", field: "disks", delete: "disk_delete"},
	"dns_record_create": {id: "record_id", list: "dns_records", field: "records", listArgs: []string{"zone_id"}, delete: "dns_record_delete"},
	"dns_zone_create":   {id: "zone_id", list: "dns_zones", field: "zones", delete: "dns_zone_delete"},
	"image_create":      {id: "image.id", lost: "image_id", list: "image_list", field: "images", delete: "image_delete"},
	"k8s_add_addon":     {none: "an addon of a cluster, known by its name; it goes with the cluster"},
	"k8s_add_nodepool":  {id: "nodepool_id", list: "k8s_nodepools", field: "node_pools", listArgs: []string{"cluster_id"}},
	"k8s_create":        {id: "cluster_id", list: "k8s_list", field: "clusters", delete: "k8s_delete"},
	"kafka_create":      {id: "cluster_id", list: "kafka_list", field: "clusters", delete: "kafka_delete"},
	"lb_create":         {id: "lb_id", list: "lb_list", field: "load_balancers", delete: "lb_delete"},
	"pg_backup_create":  {id: "backup_id", list: "pg_backups", field: "backups", listArgs: []string{"cluster_id"}},
	"pg_create":         {id: "cluster_id", list: "pg_list", field: "clusters", delete: "pg_delete"},
	"pg_db_create":      {arg: "name", delete: "pg_db_delete"},
	"pg_user_create":    {arg: "name", delete: "pg_user_delete"},
	"provision":         {id: "vm_id", list: "vm_list", field: "vms", delete: "destroy"},
	"redis_create":      {id: "cluster_id", list: "redis_list", field: "clusters", delete: "redis_delete"},
	"s3_bucket_create":  {arg: "bucket", delete: "s3_bucket_delete"},
	"s3_put":            {arg: "key", delete: "s3_rm"},
	"secret_create":     {arg: "path", lost: "secret_id", list: "secret_list", field: "secrets", delete: "secret_delete"},
	"snat_create":       {id: "nat_gateway.id", lost: "nat_gateway_id", list: "snat_list", field: "nat_gateways", delete: "snat_delete"},
	"tg_create":         {id: "tg_id", list: "tg_list", field: "target_groups", delete: "tg_delete"},
	"vpc_create":        {id: "vpc_id", list: "vpc_list", field: "vpcs", delete: "vpc_delete"},
	"vpc_route_create":  {id: "route_id", list: "vpc_routes", field: "routes", listArgs: []string{"vpc_id"}, delete: "vpc_route_delete"},
}

// createdID is the id of what tool made, as its answer res gives it; ""
// when the answer names none (an error, or a create with arg or none).
func createdID(tool string, res map[string]any) string {
	c := createdIDs[tool]
	if c.id == "" {
		return ""
	}
	for _, path := range []string{c.id, c.lostField()} {
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
