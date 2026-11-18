// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	stderrors "errors"
	"slices"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Why an optional string argument of a mutating tool takes an empty value:
// it does not name the place or the kind of what the call makes inside the
// project (Decl.Place), so an empty value is not a default of a place.
const (
	notPlaceText   = "free text the person writes (a description, a body, a value): empty is a value, evo takes no default for it"
	notPlaceName   = "a name: without one evo makes one; it names the resource, it does not place it"
	notPlaceTarget = "names a resource that is there, which the call acts on or adopts, not where or of what kind the new one is"
	notPlaceLocal  = "a file or a hash of the folder of the call, not a resource of the project"
	notPlaceK8s    = "a place or a version inside the cluster, which helm and kubectl default: not a resource of the project (out of #1597)"
	notPlaceSource = "the source of access, not a place inside the project (out of #1597; named in the PR of D100 as left)"
	notPlaceDB     = "a setting inside the service the service defaults, not a place or a kind of the resource (out of #1597)"
)

// notAPlace are the optional string arguments of mutating tools that are
// not Place, each with why. A new one is named here or in Decl.Place: the
// test fails on one that is neither.
var notAPlace = map[string]string{
	"backup_create.description":            notPlaceText,
	"backup_restore.description":           notPlaceText,
	"ca_deploy.description":                notPlaceText,
	"cert_import.description":              notPlaceText,
	"cert_import.intermediate_certificate": notPlaceText,
	"cert_issue_le.description":            notPlaceText,
	"dns_zone_create.description":          notPlaceText,
	"image_create.description":             notPlaceText,
	"image_create.display_name":            notPlaceText,
	"image_create.user_data_template":      notPlaceText,
	"kafka_create.description":             notPlaceText,
	"pg_create.description":                notPlaceText,
	"redis_create.description":             notPlaceText,
	"s3_put.content":                       notPlaceText,
	"s3_put.content_base64":                notPlaceText,
	"s3_put.content_type":                  notPlaceText,
	"secret_create.description":            notPlaceText,
	"sg_rule_create.description":           notPlaceText,
	"subnet_create.description":            notPlaceText,
	"vpc_create.description":               notPlaceText,
	"vpc_route_create.description":         notPlaceText,
	"dns_zone_create.name":                 notPlaceName,
	"k8s_add_nodepool.name":                notPlaceName,
	"k8s_create.name":                      notPlaceName,
	"k8s_create.product":                   notPlaceName + " (product goes into the default name)",
	"provision.name":                       notPlaceName,
	"destroy.name":                         notPlaceTarget,
	"destroy.vm_id":                        notPlaceTarget,
	"deploy_apply.vm_id":                   notPlaceTarget,
	"disk_create.attach_to":                notPlaceTarget,
	"provision.vm_id":                      notPlaceTarget,
	"sg_rule_create.remote_group":          notPlaceTarget + "; with cidr, one of the two is the source",
	"deploy.compose_file":                  notPlaceLocal,
	"deploy.compose_hash":                  notPlaceLocal,
	"k8s_apply.namespace":                  notPlaceK8s,
	"k8s_helm.chart":                       notPlaceK8s,
	"k8s_helm.namespace":                   notPlaceK8s,
	"k8s_helm.release":                     notPlaceK8s,
	"k8s_helm.repo_url":                    notPlaceK8s,
	"k8s_helm.values":                      notPlaceText,
	"k8s_helm.version":                     notPlaceK8s,
	"close_ports.cidr":                     notPlaceSource,
	"deploy.cidr":                          notPlaceSource,
	"open_ports.cidr":                      notPlaceSource,
	"provision.cidr":                       notPlaceSource,
	"sg_rule_create.cidr":                  notPlaceSource,
	"pg_create.backup_schedule":            notPlaceDB,
	"pg_create.initial_database":           notPlaceDB,
	"pg_db_create.lc_collate":              notPlaceDB,
	"pg_db_create.lc_ctype":                notPlaceDB,
}

// An optional string argument that names the place or the kind of what a
// mutating tool makes inside the project takes no empty value: USAGE by
// the schema, before the handler, the preview and any request, as a scope
// argument (D100; FB19I-37, #1597). The arguments are Decl.Place; every
// other optional string argument of a mutating tool is in notAPlace with
// why, so a new one is classed before it ships.
func TestPlaceArgs_EmptyIsUsage(t *testing.T) {
	seen, places := map[string]bool{}, 0
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, arg := range tl.Decl.Place {
			places++
			p, ok := props[arg].(map[string]any)
			if !ok || p["type"] != "string" {
				t.Errorf("%s: Place names %s, which is not a string argument", tl.Name, arg)
				continue
			}
			if p["minLength"] != 1 {
				t.Errorf("%s.%s (place): the schema declares no minLength 1", tl.Name, arg)
			}
			for _, empty := range []string{"", "  "} {
				err := CheckArgs(tl, map[string]any{arg: empty})
				var re *ruleError
				var ce *clierrors.CLIError
				if !stderrors.As(err, &re) || re.Arg != arg || !stderrors.As(err, &ce) || ce.Code != clierrors.CodeUsage {
					t.Errorf("%s.%s = %q (place): %v, want USAGE naming the argument", tl.Name, arg, empty, err)
				}
			}
		}
		if !tl.Mutating {
			continue
		}
		required, _ := tl.InputSchema["required"].([]string)
		for arg, v := range props {
			p, _ := v.(map[string]any)
			if p["type"] != "string" || p["enum"] != nil || slices.Contains(required, arg) || slices.Contains(tl.Decl.Place, arg) || slices.Contains(scopeArgs, arg) {
				continue
			}
			oneOf := false
			for _, set := range tl.Decl.RequiredOneOf {
				oneOf = oneOf || slices.Contains(set, arg)
			}
			if oneOf {
				continue
			}
			key := tl.Name + "." + arg
			seen[key] = true
			if notAPlace[key] == "" {
				t.Errorf("%s: an optional string argument of a mutating tool, neither in Decl.Place nor in notAPlace: does its absence take a default of a place or a kind (Place), or why not (notAPlace)?", key)
			}
		}
	}
	for key := range notAPlace {
		if !seen[key] {
			t.Errorf("notAPlace names %s, which is no longer an optional string argument of a mutating tool", key)
		}
	}
	if places < 30 {
		t.Fatalf("checked %d place arguments", places)
	}
}
