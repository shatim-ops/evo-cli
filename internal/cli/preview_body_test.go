// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// The class of a value evo sends that its preview does not name (release
// plan 1.5, С1; audit blind-spots-1.5 §9): what the person agrees to
// without seeing it.
const (
	unnamedAccess = "доступ" // opens the resource to the internet or another party
	unnamedPrice  = "цена"   // what is paid for: size, count, retention
	unnamedWork   = "работа" // whether the resource works as meant
	unnamedNoise  = "шум"    // a form of what the preview already says: an id of a named thing, a name evo makes
	unnamedSecret = "секрет" // never in a preview: the test checks it is not there
)

// bodyUnnamed are the values the confirmed call sends the cloud that its
// preview does not name — "tool path" with the class and why. The list
// only shrinks: the preview of С4 («разработка», access first) names the
// value and the line goes; a line whose value the preview names now, or
// that is not sent any more, fails the test. Secret lines stay: the value
// must not reach the preview.
// Closes by 1.8: a line lives at most two versions (release plan 1.6,
// «храповики со сроком»; TestMisses_HaveADeadline).
var bodyUnnamed = map[string]struct{ class, why string }{

	"deploy availability_zone_id": {unnamedNoise, "id зоны, которую превью называет по имени"},
	"deploy interface_id":         {unnamedNoise, "интерфейс созданной ВМ"},
	"deploy name":                 {unnamedNoise, "имя floating IP, которое evo даёт само"},
	"deploy description":          {unnamedNoise, "подпись правила «evo: port N»"},
	"deploy direction":            {unnamedNoise, "правило входящее"},
	"deploy ether_type":           {unnamedNoise, "IPv4"},
	"deploy ip_protocol":          {unnamedWork, "порты compose открываются только как TCP"},
	"deploy port_range":           {unnamedNoise, "порты compose в форме API"},

	"deploy_apply availability_zone_id": {unnamedNoise, "id зоны плана"},
	"deploy_apply interface_id":         {unnamedNoise, "интерфейс созданной ВМ"},
	"deploy_apply name":                 {unnamedNoise, "имя floating IP, которое evo даёт само"},

	"disk_attach vm_id":                {unnamedNoise, "id ВМ, которую превью называет по имени"},
	"disk_create availability_zone_id": {unnamedNoise, "id зоны, которую превью называет по имени"},
	"disk_create size":                 {unnamedNoise, "size_gb в форме API"},
	"disk_detach vm_id":                {unnamedNoise, "id ВМ, которую превью называет по имени"},
	"disk_resize size":                 {unnamedNoise, "size_gb в форме API"},

	"dns_record_create public_zone_id": {unnamedNoise, "id зоны, которую превью называет по имени"},

	"fip_attach interface_id": {unnamedNoise, "интерфейс ВМ, которую превью называет по имени"},
	"fip_attach name":         {unnamedNoise, "имя адреса при привязке"},

	"image_create availability_zones.availability_zone_id": {unnamedNoise, "id зоны, которую превью называет по имени"},

	"k8s_add_addon resolveMode": {unnamedWork, "конфликты ресурсов не разрешаются"},

	"k8s_add_nodepool updateConfiguration.strategy":  {unnamedWork, "обновление узлов по одному (ROLLING_UPDATE)"},
	"k8s_add_nodepool machineConfiguration.flavorId": {unnamedNoise, "id flavor, который превью называет по имени"},

	"k8s_create controlPlaneConfiguration.sizingConfiguration.flavorId": {unnamedNoise, "id flavor мастеров, который превью называет по имени"},
	"k8s_create controlPlaneConfiguration.zones":                        {unnamedNoise, "id зоны, которую превью называет по имени"},

	"lb_create rules.listeners.name":        {unnamedNoise, "имя listener, которое evo даёт само"},
	"lb_create rules.listeners.target_port": {unnamedWork, "порт цели равен порту listener"},
	"lb_create rules.name":                  {unnamedNoise, "имя правила, которое evo даёт само"},

	"open_ports description": {unnamedNoise, "подпись правила"},
	"open_ports direction":   {unnamedNoise, "правило входящее"},
	"open_ports ether_type":  {unnamedNoise, "IPv4"},
	"open_ports port_range":  {unnamedNoise, "порты в форме API"},

	"provision availability_zone_id": {unnamedNoise, "id зоны, которую превью называет по имени"},
	"provision interface_id":         {unnamedNoise, "интерфейс созданной ВМ"},
	"provision name":                 {unnamedNoise, "имя floating IP, которое evo даёт само"},

	"secret_add_version payload": {unnamedSecret, "значение секрета"},
	"secret_create payload":      {unnamedSecret, "значение секрета"},

	"stack_apply name": {unnamedNoise, "имя версии задания"},

	"vpc_route_create nextHop.virtualMachine.interface.id":      {unnamedNoise, "интерфейс ВМ, которую превью называет по имени"},
	"vpc_route_create nextHop.virtualMachine.virtualMachine.id": {unnamedNoise, "id ВМ, которую превью называет по имени"},
}

// bodyNamedAs are the values the preview names under the argument of the
// tool they come from, not under the API's word: "tool path" → the key.
// The value must match, as for any key; a line no write reaches fails.
var bodyNamedAs = map[string]string{
	"ar_create isPublic": "public",
	"ca_deploy configuration.ingress.publiclyAccessible": "public",
	"lb_create external_address_spec.allocate":           "external",

	"ca_deploy template.containers.containerPort":        "port",
	"ca_deploy template.scaling.maxInstanceCount":        "max_instances",
	"ca_deploy template.scaling.minInstanceCount":        "min_instances",
	"disk_create disk_type_name":                         "type",
	"k8s_add_addon appVersion":                           "version",
	"k8s_add_nodepool machineConfiguration.disk.size":    "disk_gb",
	"k8s_create networkConfiguration.privateVipSubnetId": "subnet_id",
	"lb_create availability_zone_specs.subnet_ids":       "subnet_id",
	"subnet_create subnet_address":                       "cidr",
	"pg_create subnet_ids":                               "subnet_id",
	"pg_create storage.pg_data_gb":                       "storage_gb",
	"pg_create backup.schedule":                          "backup_schedule",
	"pg_create backup.retention_policy_days":             "backup_retention_days",

	"open_ports remote_ip_prefix": "cidr",
	"open_ports ip_protocol":      "protocol",
	"deploy remote_ip_prefix":     "ports_source",
}

// bodyUnnamedEverywhere are fields no preview names, one line for every
// tool that sends them.
var bodyUnnamedEverywhere = map[string]struct{ class, why string }{
	// The project of the profile or the key: no preview names it, though a
	// wrong project is the costliest mistake of a create.
	"projectid": {unnamedWork, "проект профиля или ключа; превью его не называет"},
}

// Release plan 1.5, С1 (audit blind-spots-1.5, §9: «the preview does not
// say what evo chose»): every value the confirmed call of a mutating tool
// sends the cloud is named in its preview with the same value — a key of
// the preview (any depth, any of snake_case or camelCase) or an argument of
// the call — or is a line of bodyUnnamed with its class. A secret is the
// other way round: its value is never in the preview. On the world of
// TestParity_JSONEqualsTool: every mutating tool with the arguments of
// parityArgs.
func TestPreview_NamesWhatTheWriteSends(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	reg := newRegistry()
	seen, tools := map[string]bool{}, map[string]bool{}
	var bad []string
	byClass := map[string]int{}
	for _, c := range cases {
		if !c.tool.Mutating || tools[c.tool.Name] || c.state != "" {
			continue
		}
		tools[c.tool.Name] = true
		tl, _ := reg.Lookup(c.tool.Name)
		call := func(confirmed bool) (string, []string) {
			args := copyArgs(c.args)
			done := func() {}
			if c.setup != nil {
				extra, d := c.setup(t, w)
				for k, v := range extra {
					args[k] = v
				}
				done = d
			}
			if confirmed {
				args["confirmed"] = true
			}
			fake.reset()
			newProcess()
			flagJSON, flagDryRun, flagYes = false, false, false
			text, _ := tl.Handler(context.Background(), args)
			writes := fake.takeWrites()
			done()
			_ = os.Remove(state.DefaultPath(w.dir))
			return text, writes
		}
		preview, _ := call(false)
		_, writes := call(true)
		named := namedValues(preview, c.args)
		for _, wr := range writes {
			body := writeBody(wr)
			for path, value := range body {
				key := c.tool.Name + " " + path
				last := path[strings.LastIndex(path, ".")+1:]
				if d, ok := bodyUnnamed[key]; ok && d.class == unnamedSecret {
					seen[key] = true
					// The API takes it base64: the preview has neither form.
					plain, _ := base64.StdEncoding.DecodeString(value)
					if strings.Contains(preview, value) || len(plain) > 0 && strings.Contains(preview, string(plain)) {
						bad = append(bad, key+": the secret value is in the preview")
					}
					continue
				}
				if named[normKey(last)][value] {
					continue
				}
				if as, ok := bodyNamedAs[key]; ok && named[normKey(as)][value] {
					seen[key] = true
					continue
				}
				if d, ok := bodyUnnamedEverywhere[normKey(last)]; ok {
					if !seen[key] {
						byClass[d.class]++
					}
					seen[key], seen["* "+normKey(last)] = true, true
					continue
				}
				if d, ok := bodyUnnamed[key]; ok {
					if !seen[key] {
						byClass[d.class]++
					}
					seen[key] = true
					continue
				}
				bad = append(bad, fmt.Sprintf("%s = %s: sent, not in the preview — name it there, or a line of bodyUnnamed with its class", key, value))
			}
		}
	}
	for _, tl := range reg.Tools() {
		if tl.Mutating && !tools[tl.Name] {
			bad = append(bad, tl.Name+": a mutating tool without a case in parityArgs")
		}
	}
	for key, d := range bodyUnnamed {
		switch {
		case !seen[key]:
			bad = append(bad, key+": in bodyUnnamed, but sent named or not sent: remove the line")
		case d.why == "":
			bad = append(bad, key+": a line without why")
		}
		switch d.class {
		case unnamedAccess, unnamedPrice, unnamedWork, unnamedNoise, unnamedSecret:
		default:
			bad = append(bad, fmt.Sprintf("%s: class %q", key, d.class))
		}
	}
	for key := range bodyNamedAs {
		if !seen[key] {
			bad = append(bad, key+": in bodyNamedAs, but not sent or not named so: remove the line")
		}
	}
	for seg := range bodyUnnamedEverywhere {
		if !seen["* "+seg] {
			bad = append(bad, seg+": in bodyUnnamedEverywhere, but no tool sends it unnamed: remove the line")
		}
	}
	t.Logf("mutating tools: %d; values not in the preview by class: %v", len(tools), byClass)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// writeBody is the JSON body of a write recorded by jsonFake ("METHOD
// url body") as leaf paths (arrays without an index) and values; nil for a
// body that is not a JSON object.
func writeBody(write string) map[string]string {
	i := strings.Index(write, " {")
	if i < 0 {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(write[i+1:]), &v) != nil {
		return nil
	}
	out := map[string]string{}
	leaves(v, "", func(path, value string) { out[path] = value })
	return out
}

// namedValues are the values the preview and the arguments of the call
// name, by key in one form (normKey).
func namedValues(preview string, args map[string]any) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	add := func(path, value string) {
		k := normKey(path[strings.LastIndex(path, ".")+1:])
		if out[k] == nil {
			out[k] = map[string]bool{}
		}
		out[k][value] = true
	}
	var v any
	if json.Unmarshal([]byte(preview), &v) == nil {
		leaves(v, "", add)
		params(v, add)
	}
	for k, a := range args {
		leaves(a, k, add)
	}
	return out
}

// params names the parameters of a preview (engine.PreviewParam, a list of
// {"key": k, "value": v}) by their keys: the value v under the key k.
func params(v any, add func(path, value string)) {
	switch x := v.(type) {
	case map[string]any:
		if k, ok := x["key"].(string); ok && len(x) == 2 {
			if val, ok := x["value"]; ok {
				leaves(val, k, add)
				return
			}
		}
		for _, e := range x {
			params(e, add)
		}
	case []any:
		for _, e := range x {
			params(e, add)
		}
	}
}

func leaves(v any, path string, f func(path, value string)) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			p := k
			if path != "" {
				p = path + "." + k
			}
			leaves(e, p, f)
		}
	case []any:
		for _, e := range x {
			leaves(e, path, f)
		}
	default:
		f(path, fmt.Sprint(x))
	}
}

// normKey is a key in one form: snake_case and camelCase alike.
func normKey(k string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(k))
}
