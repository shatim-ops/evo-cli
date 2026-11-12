// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The world of a change carries its resource in every state of the
// specification (release plan 1.9, «Мир каждого ресурса несёт каждое
// состояние из спецификации»): on 12.11 three defects of one class were
// found by live runs and probes, not by tests — a finished operation with
// an error, a rule in the window of its deletion, a backup that is not
// «created» — and in each the world of the test had the resource only as
// ready. The states are Cloud.ru's own: scripts/api-contract-states.json
// holds the enums of its published specifications for the resources evo
// changes. Every tool that asks for consent names the enum its answer is
// decided by, and has a row — what it answers — for every state of it.

// specStatesOf names, for a tool that changes the cloud, the enum of the
// states of the resource its answer is decided by: a key of
// scripts/api-contract-states.json.
var specStatesOf = map[string]string{
	"ar_create": "ar:v1RegistryStatus", "ar_delete": "ar:v1RegistryStatus",

	"cert_delete": "certificatemanager:v1CertificateState", "cert_import": "certificatemanager:v1CertificateState",
	"cert_issue_le": "certificatemanager:v1CertificateState", "cert_renew": "certificatemanager:v1CertificateState",

	"close_ports": "compute:SecurityGroupRuleState", "open_ports": "compute:SecurityGroupRuleState",
	"sg_rule_create": "compute:SecurityGroupRuleState", "sg_rule_delete": "compute:SecurityGroupRuleState",

	"deploy": "compute:VmState", "deploy_apply": "compute:VmState", "destroy": "compute:VmState", "provision": "compute:VmState",
	"vm_start": "compute:VmState", "vm_stop": "compute:VmState", "vm_reboot": "compute:VmState",

	"disk_attach": "compute:DiskState", "disk_create": "compute:DiskState", "disk_delete": "compute:DiskState",
	"disk_detach": "compute:DiskState", "disk_resize": "compute:DiskState",

	"fip_attach": "compute:FloatingIpState", "release_floating_ip": "compute:FloatingIpState",
	"snat_create": "compute:NATGatewayState", "snat_delete": "compute:NATGatewayState",
	"subnet_create": "compute:SubnetState", "subnet_delete": "compute:SubnetState",
	"vpc_route_create": "vpc:StaticRoute.status", "vpc_route_delete": "vpc:StaticRoute.status",

	"k8s_create": "mk8s:cloudru.containers.mk8s.v3.Cluster.status", "k8s_delete": "mk8s:cloudru.containers.mk8s.v3.Cluster.status",
	"k8s_add_nodepool": "mk8s:cloudru.containers.mk8s.v3.NodePool.status", "k8s_nodepool_delete": "mk8s:cloudru.containers.mk8s.v3.NodePool.status", "k8s_nodepool_scale": "mk8s:cloudru.containers.mk8s.v3.NodePool.status",
	"k8s_add_addon": "mk8s:cloudru.containers.mk8s.v3.ClusterAddon.status",

	"kafka_create": "kafka:cloudru.dbaas.kafka.v1.Cluster.status", "kafka_delete": "kafka:cloudru.dbaas.kafka.v1.Cluster.status",
	"redis_create": "redis:cloudru.dbaas.redis.v1.Cluster.status", "redis_delete": "redis:cloudru.dbaas.redis.v1.Cluster.status",
	"pg_create": "postgresql:cloudru.dbaas.postgresql.v1.Cluster.status", "pg_delete": "postgresql:cloudru.dbaas.postgresql.v1.Cluster.status",
	"pg_db_create": "postgresql:cloudru.dbaas.postgresql.v1.Cluster.status", "pg_db_delete": "postgresql:cloudru.dbaas.postgresql.v1.Cluster.status",
	"pg_user_create": "postgresql:cloudru.dbaas.postgresql.v1.Cluster.status", "pg_user_delete": "postgresql:cloudru.dbaas.postgresql.v1.Cluster.status",
	"pg_backup_create": "postgresql:cloudru.dbaas.postgresql.v1.Backup.status",

	"backup_delete": "compute:BackupState",

	"lb_create": "nlb:v2NLBStatus", "lb_delete": "nlb:v2NLBStatus",
	"tg_create": "nlb:v2TargetGroupStatus", "tg_delete": "nlb:v2TargetGroupStatus",

	"secret_create": "secretmanager:v1State", "secret_delete": "secretmanager:v1State",
	"secret_add_version": "secretmanager:v2VersionState",
}

// noSpecStates are the tools that change the cloud whose resource has no
// enum of states to carry: the reason. A closed list.
var noSpecStates = map[string]string{
	"ca_delete": "Container Apps: the published specification declares no enum of the app's state (read 12.11)",
	"ca_deploy": "Container Apps: no enum of the app's state in the specification",
	"ca_start":  "Container Apps: no enum of the app's state in the specification",
	"ca_stop":   "Container Apps: no enum of the app's state in the specification",

	"dns_record_create": "DNS: the specification declares no enum of a state of a zone or a record",
	"dns_record_delete": "DNS: no enum of a state in the specification",
	"dns_zone_create":   "DNS: no enum of a state in the specification",
	"dns_zone_delete":   "DNS: no enum of a state in the specification",

	"image_create": "Compute: the specification declares no enum of an image's state",
	"image_delete": "Compute: no enum of an image's state in the specification",

	"vpc_create": "VPC: the specification declares enums for routes, pools and addresses, none for the VPC itself",
	"vpc_delete": "VPC: no enum of the VPC's state in the specification",

	"k8s_apply": "acts inside a cluster through kubectl: the objects are the cluster's, not the cloud API's",
	"k8s_helm":  "acts inside a cluster through helm", "k8s_kubectl": "acts inside a cluster through kubectl",

	"s3_bucket_create": "S3: a bucket and an object have no state", "s3_bucket_delete": "S3: no state",
	"s3_put": "S3: no state", "s3_rm": "S3: no state",

	"stack_apply": "runs the tools of its steps: each has its own states", "stack_destroy": "runs the deletes of its resources: each has its own states",
}

// stateRowsOf are the rows «state → answer» a tool has, by the state: the
// tables the worlds run by. A tool joins it with its table.
var stateRowsOf = func() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for tool, rows := range vmPowerStateRows {
		out[tool] = map[string]bool{}
		for state := range rows {
			out[tool][state] = true
		}
	}
	for tool, rows := range sgRuleStateRows {
		out[tool] = map[string]bool{}
		for state := range rows {
			out[tool][state] = true
		}
	}
	out["backup_delete"] = map[string]bool{}
	for state := range backupDeleteStateRows {
		out["backup_delete"][state] = true
	}
	return out
}()

// stateWorldsPendingNew are the tools new in 1.9 with no row for every
// state of the specification yet. Only shrinks. Closes by 1.9.
var stateWorldsPendingNew = map[string]string{
	"k8s_nodepool_delete": "worlds: the group ready, the last group, deleting (D61); not the 16 states of NodePool.status",
	"k8s_nodepool_scale":  "worlds: the group ready and at the size; not the 16 states",
}

// stateWorldsPendingShipped are the tools shipped before the invariant
// with no row for every state of the specification. Only shrinks, and a
// new tool does not join it. Closes by 1.10.
var stateWorldsPendingShipped = map[string]bool{
	"ar_create": true, "ar_delete": true,
	"cert_delete": true, "cert_import": true, "cert_issue_le": true, "cert_renew": true,
	"close_ports": true, "open_ports": true,
	"deploy": true, "deploy_apply": true, "destroy": true, "provision": true,
	"disk_attach": true, "disk_create": true, "disk_delete": true, "disk_detach": true, "disk_resize": true,
	"fip_attach": true, "release_floating_ip": true,
	"snat_create": true, "snat_delete": true, "subnet_create": true, "subnet_delete": true,
	"vpc_route_create": true, "vpc_route_delete": true,
	"k8s_create": true, "k8s_delete": true, "k8s_add_nodepool": true, "k8s_add_addon": true,
	"kafka_create": true, "kafka_delete": true, "redis_create": true, "redis_delete": true,
	"pg_create": true, "pg_delete": true, "pg_db_create": true, "pg_db_delete": true,
	"pg_user_create": true, "pg_user_delete": true, "pg_backup_create": true,
	"lb_create": true, "lb_delete": true, "tg_create": true, "tg_delete": true,
	"secret_create": true, "secret_delete": true, "secret_add_version": true,
}

func TestStates_EveryChangeHasAWorldForEveryStateOfTheSpec(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "api-contract-states.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		States map[string][]string `json:"states"`
	}
	if err := json.Unmarshal(b, &file); err != nil || len(file.States) == 0 {
		t.Fatalf("scripts/api-contract-states.json: no states: %v", err)
	}
	mutating := map[string]bool{}
	used := map[string]bool{}
	var names []string
	for _, tl := range New(nil).Tools() {
		if tl.Mutating {
			mutating[tl.Name] = true
			names = append(names, tl.Name)
		}
	}
	sort.Strings(names)
	covered, pending := 0, 0
	for _, name := range names {
		enum, named := specStatesOf[name]
		_, none := noSpecStates[name]
		switch {
		case named && none:
			t.Errorf("%s: in specStatesOf and in noSpecStates — one of the two", name)
			continue
		case !named && !none:
			t.Errorf("%s changes the cloud and names no enum of states: add it to specStatesOf (a key of scripts/api-contract-states.json) with a row for every state, or to noSpecStates with the reason", name)
			continue
		case none:
			continue
		}
		states := file.States[enum]
		if len(states) == 0 {
			t.Errorf("%s: specStatesOf names %q, which scripts/api-contract-states.json does not hold", name, enum)
			continue
		}
		used[enum] = true
		rows := stateRowsOf[name]
		_, isNew := stateWorldsPendingNew[name]
		isPending := isNew || stateWorldsPendingShipped[name]
		if rows == nil {
			if !isPending {
				t.Errorf("%s: no table «state → answer» (stateRowsOf) for the %d states of %s — a world for each: what the tool answers for a resource that is being made, in error, being deleted", name, len(states), enum)
			}
			pending++
			continue
		}
		if isPending {
			t.Errorf("%s has its table of states now: remove it from the pending list (the list only shrinks)", name)
		}
		for _, state := range states {
			if !rows[state] {
				t.Errorf("%s: the specification's state %q of %s has no row — say what the tool answers for it", name, state, enum)
			}
		}
		covered++
	}
	for _, list := range []map[string]bool{nameSetOf(specStatesOf), nameSetOf(noSpecStates), nameSetOf(stateWorldsPendingNew), stateWorldsPendingShipped} {
		for name := range list {
			if !mutating[name] {
				t.Errorf("the lists of the test name %s, which is no tool that changes the cloud: remove the line", name)
			}
		}
	}
	for enum := range file.States {
		if !used[enum] {
			t.Errorf("scripts/api-contract-states.json holds %q, which no tool names (specStatesOf): remove it, or name the tool", enum)
		}
	}
	t.Logf("states of the specification: %d tools with a row for every state, %d pending", covered, pending)
}

// nameSetOf is the set of the keys of m.
func nameSetOf(m map[string]string) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}
