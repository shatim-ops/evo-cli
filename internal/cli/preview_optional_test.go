// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// optionalArgs are the values the second argument set of a mutating tool
// gives its optional arguments where the one optionalValue makes up would
// not reach the write in the world (a value of the world's catalogue, an
// address, a port a service allows): "tool" → argument → value.
var optionalArgs = map[string]map[string]any{
	"ca_deploy":        {"port": float64(8080)},
	"close_ports":      {"cidr": "0.0.0.0/0", "protocol": "tcp"},
	"open_ports":       {"cidr": "10.0.0.0/8"},
	"deploy":           {"flavor": "lowcost10-4-8", "image_id": pvImage, "region": pvZone, "project_id": pvProject, "compose_file": "docker-compose.yml"},
	"provision":        {"flavor": "lowcost10-4-8", "image_id": pvImage, "region": pvZone, "project_id": pvProject, "subnet_id": optionalSubnet},
	"disk_create":      {"attach_to": parityVM, "zone": pvZone},
	"image_create":     {"zone": pvZone},
	"k8s_add_addon":    {"resolve_mode": "OVERWRITE", "version": "1.1.0"},
	"k8s_add_nodepool": {"disk_gb": float64(20), "nodes_subnet_id": optionalSubnet},
	"k8s_create":       {"subnet_id": optionalSubnet, "master_count": float64(3), "version": "v1.31.4"},
	"k8s_helm":         {"repo_url": "https://charts.example.invalid"},
	"kafka_create":     {"version_id": "v1", "specification_id": "s-1", "subnet_id": optionalSubnet},
	"redis_create":     {"type": "MASTER_REPLICA", "replicas": float64(2), "version_id": "v1", "specification_id": "s-1", "subnet_id": optionalSubnet},
	"pg_create":        {"storage_gb": float64(30), "version": "17", "subnet_id": optionalSubnet, "backup_schedule": "0 3 * * *"},
	"lb_create":        {"subnet_id": optionalSubnet},
}

// optionalSubnet is the subnet of the world's project.
const optionalSubnet = "00000000-0000-0000-0000-0000000000d1"

// optionalLeftOut are the optional arguments the second set leaves out:
// "tool argument" → why. An alternative of another argument (image or
// image_id) goes here, the other one is set.
var optionalLeftOut = map[string]string{
	"deploy image":                              "an alternative of image_id",
	"provision image":                           "an alternative of image_id",
	"destroy name":                              "an alternative of vm_id",
	"k8s_add_nodepool flavor":                   "an alternative of cpu and ram_gb",
	"k8s_create flavor":                         "an alternative of cpu and ram_gb",
	"release_floating_ip ip_address":            "an alternative of id",
	"s3_put content_base64":                     "an alternative of content",
	"vpc_route_create next_hop_vip_id":          "an alternative of next_hop_vm_id",
	"vpc_route_create next_hop_magic_router_id": "an alternative of next_hop_vm_id",
}

// optionalUnnamed are the optional arguments known to reach the write
// unnamed by the preview: "tool argument" → the work item of the release
// plan that names them. The list only shrinks: an argument not here fails
// the test, and so does a line whose argument the preview names now.
var optionalUnnamed = map[string]string{
	"cert_import description":              "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"cert_import intermediate_certificate": "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"cert_issue_le description":            "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"dns_zone_create description":          "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"image_create description":             "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"image_create display_name":            "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"image_create user_data_template":      "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"kafka_create description":             "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"lb_create target_port":                "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"pg_create description":                "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"pg_db_create lc_collate":              "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"pg_db_create lc_ctype":                "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"redis_create description":             "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"vpc_create description":               "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
	"vpc_route_create description":         "release plan 1.6, «Превью называет заданные необязательные аргументы» (С1, второй набор): «разработка»",
}

// optionalValue is a value of an optional argument from its schema: the
// last of an enum that is not the default, true for a boolean that is not
// true by default, the minimum plus one for a number, an id of the world
// for a uuid, else a plain string.
func optionalValue(p map[string]any) (any, bool) {
	if e, ok := p["enum"].([]any); ok && len(e) > 0 {
		v := e[len(e)-1]
		if d, ok := p["default"]; ok && fmt.Sprint(d) == fmt.Sprint(v) && len(e) > 1 {
			v = e[0]
		}
		return v, true
	}
	switch p["type"] {
	case "boolean":
		d, _ := p["default"].(bool)
		return !d, true
	case "integer", "number":
		if m, ok := p["minimum"].(float64); ok {
			return m + 1, true
		}
		return float64(2), true
	case "string":
		if p["format"] == "uuid" {
			return parityVM, true
		}
		return "evo-opt", true
	}
	return nil, false
}

// Release plan 1.6 (С1, second argument set; FB150-5 of the full audit of
// 1.5: secret_create sent kms_key_id and description its preview did not
// name, and С1 did not see it — the world passed no optional argument).
// Every mutating tool is called a second time with each of its optional
// arguments set (optionalValue, optionalArgs, minus optionalLeftOut): the
// call reaches the write, and each optional argument the write sends is
// named in the preview with the value sent — the preview alone, not the
// arguments of the call — or is a line of optionalUnnamed.
func TestPreview_NamesEveryOptionalArgument(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	reg := newRegistry()
	done := map[string]bool{}
	used := map[string]bool{}
	found := map[string]string{}
	var bad []string
	for _, c := range cases {
		if !c.tool.Mutating || done[c.tool.Name] || c.state != "" {
			continue
		}
		done[c.tool.Name] = true
		full, set := fullOptionalArgs(c.tool, c.args, used)
		if len(set) == 0 {
			continue
		}
		tl, _ := reg.Lookup(c.tool.Name)
		call := func(confirmed bool) (string, []string) {
			args := copyArgs(full)
			end := func() {}
			if c.setup != nil {
				extra, d := c.setup(t, w)
				for k, v := range extra {
					args[k] = v
				}
				end = d
			}
			if confirmed {
				args["confirmed"] = true
			}
			fake.reset()
			newProcess()
			flagJSON, flagDryRun, flagYes = false, false, false
			text, _ := tl.Handler(context.Background(), args)
			writes := fake.takeWrites()
			end()
			_ = os.Remove(state.DefaultPath(w.dir))
			return text, writes
		}
		preview, _ := call(false)
		answer, writes := call(true)
		if len(writes) == 0 {
			bad = append(bad, fmt.Sprintf("%s: the call with every optional argument writes nothing — a value of optionalArgs, or a line of optionalLeftOut: %s", c.tool.Name, clipLine(answer)))
			continue
		}
		// The preview alone: an argument is not named by being given.
		named := namedValues(preview, nil)
		for _, wr := range writes {
			for path, value := range writeBody(wr) {
				last := normKey(path[strings.LastIndex(path, ".")+1:])
				for _, a := range set {
					if normKey(a) != last || fmt.Sprint(full[a]) != value || named[last][value] {
						continue
					}
					found[c.tool.Name+" "+a] = value
				}
			}
		}
	}
	for tool, args := range optionalArgs {
		for a := range args {
			if !used[tool+" "+a] {
				bad = append(bad, tool+" "+a+": in optionalArgs, but not an optional argument of a mutating tool: remove the line")
			}
		}
	}
	for key := range optionalLeftOut {
		if !used[key] {
			bad = append(bad, key+": in optionalLeftOut, but not an optional argument of a mutating tool: remove the line")
		}
	}
	for key, value := range found {
		if optionalUnnamed[key] == "" {
			bad = append(bad, fmt.Sprintf("%s = %s: sent, not in the preview — name it there, or, while the plan moves it, a line of optionalUnnamed with the work item", key, value))
		}
	}
	for key := range optionalUnnamed {
		if found[key] == "" {
			bad = append(bad, key+": in optionalUnnamed, but the preview names it now (or it is not sent): remove the line")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
	w.rt.takeUnknown()
}

// fullOptionalArgs is base with every optional argument of tl set, and the
// names of those set; used records the lines of optionalArgs and
// optionalLeftOut it took.
func fullOptionalArgs(tl tools.Tool, base map[string]any, used map[string]bool) (map[string]any, []string) {
	full := copyArgs(base)
	var set []string
	for k, raw := range schemaProps(tl) {
		key := tl.Name + " " + k
		if _, has := base[k]; has || k == "confirmed" {
			continue
		}
		if _, out := optionalLeftOut[key]; out {
			used[key] = true
			continue
		}
		v, ok := optionalArgs[tl.Name][k]
		if ok {
			used[key] = true
		} else {
			p, _ := raw.(map[string]any)
			if v, ok = optionalValue(p); !ok {
				continue
			}
		}
		full[k] = v
		set = append(set, k)
	}
	sort.Strings(set)
	return full, set
}
