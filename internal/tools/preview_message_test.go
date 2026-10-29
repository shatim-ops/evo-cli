// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// gatedPreviewArgs adjusts contractArgs so the call gets past the checks
// before the gate: the type sweep fills every argument, and some pairs
// exclude each other (image and image_id) or need a valid value
// (master_count).
var gatedPreviewArgs = map[string]func(a map[string]any){
	"deploy":              func(a map[string]any) { delete(a, "image") },
	"provision":           func(a map[string]any) { delete(a, "image") },
	"destroy":             func(a map[string]any) { delete(a, "vm_id"); delete(a, "name") }, // the current deployment
	"image_create":        func(a map[string]any) { delete(a, "image_metadata") },
	"disk_create":         func(a map[string]any) { delete(a, "attach_to") },
	"release_floating_ip": func(a map[string]any) { delete(a, "ip_address") },
	"tg_create":           func(a map[string]any) { delete(a, "targets") },
	"vpc_route_create": func(a map[string]any) {
		delete(a, "next_hop_vip_id")
		delete(a, "next_hop_magic_router_id")
		delete(a, "next_hop_az")
	},
	"dns_record_create": func(a map[string]any) { delete(a, "values") },
	"cert_issue_le":     func(a map[string]any) { a["dns_names"] = []any{"web.example.com"} },
	"lb_create":         func(a map[string]any) { a["zone"] = contractID; delete(a, "subnet_id") },
}

// gatedPreviewBody is the 200 answer of the cloud for the tools whose
// preview finds its argument in a list (snat_delete: the gateway; lb_create:
// the zone).
const gatedPreviewBody = `{"items":[{"id":"` + contractID + `","name":"web","status":"ACTIVE"}],` +
	`"total":1,"id":"` + contractID + `","name":"web","done":true,"status":"ACTIVE"}`

// gatedPreviewFixtures drive the tools whose preview needs answers of their
// own API that the generic sweep has not (the addon catalogue, the DBaaS
// specifications): each builds its registry on its fake (which takes
// http.DefaultTransport) and returns the tool's answer.
var gatedPreviewFixtures = map[string]func(t *testing.T) string{
	// A delete asks consent for what the cloud has; what it does not have
	// is already deleted (D26): these look their resource up in its list.
	"disk_delete": func(t *testing.T) string { return liveDelete(t, "disk_delete", map[string]any{"disk_id": liveID}) },
	// subnet_create needs the project's VPC and zone (subnet_test.go).
	"subnet_create": func(t *testing.T) string {
		_, text, _ := callNamed(t, subnetRegistry(t, newSubnetAPI()), "subnet_create", map[string]any{"name": "web", "cidr": "10.10.0.0/24", "zone": "ru.AZ-1"})
		return text
	},
	"pg_db_delete": func(t *testing.T) string {
		return liveDelete(t, "pg_db_delete", map[string]any{"cluster_id": liveID, "name": liveName})
	},
	"pg_user_delete": func(t *testing.T) string {
		return liveDelete(t, "pg_user_delete", map[string]any{"cluster_id": liveID, "name": liveName})
	},
	"vpc_route_delete": func(t *testing.T) string {
		return liveDelete(t, "vpc_route_delete", map[string]any{"vpc_id": liveID, "route_id": liveID})
	},
	"s3_rm": func(t *testing.T) string {
		return liveDelete(t, "s3_rm", map[string]any{"bucket": liveName, "key": liveName})
	},
	// S3 deletes only an empty bucket: the plan refuses one with objects
	// before the question (W6), so the bucket of the preview is empty.
	"s3_bucket_delete": func(t *testing.T) string {
		f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Host != "s3.cloud.ru" && !strings.HasSuffix(r.URL.Host, ".s3.cloud.ru") {
				return liveWorld(w, r)
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`+
				`<Name>`+liveName+`</Name><KeyCount>0</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>`)
			return true
		}}
		_, text, _ := callNamed(t, apiFakeS3Registry(t, f), "s3_bucket_delete", map[string]any{"bucket": liveName})
		return text
	},
	// The preview of a new VM reads the flavors, the image and the price
	// (spec REQ-7): on the generic answers the image is not the project's,
	// and the preview refuses it, as provision would after consent.
	// The preview of a cluster resolves its plan, of a node pool its
	// flavor, from the catalogue of Managed Kubernetes (D29).
	"k8s_create": func(t *testing.T) string {
		_, text, _ := callNamed(t, raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, nil), "k8s_create",
			map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1"})
		return text
	},
	"k8s_add_nodepool": func(t *testing.T) string {
		_, text, _ := callNamed(t, raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, nil), "k8s_add_nodepool",
			map[string]any{"cluster_id": "cl-1", "security_group_id": "sg-1"})
		return text
	},
	"provision": func(t *testing.T) string {
		r, _ := cancelRegistry(t, &productCloud{})
		_, text, _ := callNamed(t, r, "provision", map[string]any{"name": "web"})
		return text
	},
	"deploy": func(t *testing.T) string {
		r, _ := cancelRegistry(t, &productCloud{})
		_, text, _ := callNamed(t, r, "deploy", map[string]any{})
		return text
	},
	// open_ports reads its group and the group's rules before the question
	// (G13-23, D35): the zone's group of the fault cloud.
	"open_ports": func(t *testing.T) string {
		r, _ := faultToolsRegistry(t, &faultCloud{})
		_, text, _ := callNamed(t, r, "open_ports", map[string]any{"security_group": "SSH-access_ru.AZ-1", "ports": []any{float64(8080)}})
		return text
	},
	// fip_attach finds the address and the VM before the question (W6): a
	// free address and a VM with its interface in the fault cloud.
	"fip_attach": func(t *testing.T) string {
		c := &faultCloud{
			vms:  []faultVM{{ID: faultID(0x41), Name: "web", Status: "running"}},
			fips: []faultFIP{{ID: faultID(0x42), IP: "203.0.113.10", State: "available"}},
		}
		r, _ := faultToolsRegistry(t, c)
		_, text, _ := callNamed(t, r, "fip_attach", map[string]any{"fip": "203.0.113.10", "vm": faultID(0x41)})
		return text
	},
	// disk_attach and disk_detach find the VM and read the disk's state
	// before the question (D53): a VM and a settled disk.
	"disk_attach": func(t *testing.T) string {
		r := transientDiskRegistry(&transientDiskCloud{faultCloud: &faultCloud{}, from: "available"})
		_, text, _ := callNamed(t, r, "disk_attach", diskChanges["disk_attach"])
		return text
	},
	"disk_detach": func(t *testing.T) string {
		r := transientDiskRegistry(&transientDiskCloud{faultCloud: &faultCloud{}, from: "in_use"})
		_, text, _ := callNamed(t, r, "disk_detach", diskChanges["disk_detach"])
		return text
	},
	// destroy with no deployment answers status none with no question, as
	// the CLI (W5): its preview needs a deployment in the state.
	"destroy": func(t *testing.T) string {
		r, _ := targetRegistry(t, &targetCloud{})
		_, text, _ := callNamed(t, r, "destroy", map[string]any{})
		return text
	},
	// close_ports reads its group and the rules it would delete before the
	// question (D37): the zone's group with the rule of port 80.
	"close_ports": func(t *testing.T) string {
		r, _ := faultToolsRegistry(t, &faultCloud{rules: []provision.SGRule{
			{ID: faultID(0x72), Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80", RemoteIPPrefix: "0.0.0.0/0"},
		}})
		_, text, _ := callNamed(t, r, "close_ports", map[string]any{"security_group": "SSH-access_ru.AZ-1", "ports": []any{float64(80)}})
		return text
	},
	// The preview of deploy_apply is its saved plan (B12-8): a plan_id
	// with no plan file is refused before the gate.
	"deploy_apply": func(t *testing.T) string {
		r, dir := faultToolsRegistry(t, &faultCloud{})
		plan := faultPlan()
		if err := deployplan.Save(dir, plan); err != nil {
			t.Fatal(err)
		}
		_, text, _ := callNamed(t, r, "deploy_apply", map[string]any{"plan_id": plan.PlanID})
		return text
	},
	"k8s_add_addon": func(t *testing.T) string {
		f := &fakeMK8s{pools: `{"nodePools":[{"clusterId":"` + addonCluster + `","scalePolicy":{"fixedScale":{"count":2}}}]}`}
		_, text, _ := callNamed(t, addonRegistry(t, f), "k8s_add_addon", map[string]any{"cluster_id": addonCluster, "name": "cert-manager"})
		return text
	},
	"kafka_create": func(t *testing.T) string {
		_, text, _ := callNamed(t, dbaasRegistry(t, &fakeDbaas{}, ""), "kafka_create", map[string]any{"name": "c1", "cpu": 2, "ram": 4})
		return text
	},
	"redis_create": func(t *testing.T) string {
		_, text, _ := callNamed(t, dbaasRegistry(t, &fakeDbaas{}, ""), "redis_create", map[string]any{"name": "c1", "cpu": 2, "ram": 4})
		return text
	},
	"pg_create": func(t *testing.T) string {
		f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Host != "postgresql.api.cloud.ru" {
				return false
			}
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/v1/clusters":
				_, _ = io.WriteString(w, `{"clusters":[]}`)
			case r.Method == http.MethodGet && r.URL.Path == "/v1/versions":
				_, _ = io.WriteString(w, `{"versions":[{"id":"v17","name":"17"}]}`)
			case r.Method == http.MethodGet && r.URL.Path == "/v1/specifications":
				_, _ = io.WriteString(w, `{"specifications":[{"id":"std","display_name":"std","cpu":2,"memory":4,"deployment_mode":"STANDARD","min_storage_gb":20,"max_hosts":2}]}`)
			default:
				http.Error(w, `{"message":"no such path"}`, 404)
			}
			return true
		}}
		_, text, _ := callNamed(t, apiFakeRegistry(t, f), "pg_create", map[string]any{"name": "web", "specification_id": "std"})
		return text
	},
}

// The message of a preview (status confirmation_required) describes the
// action; it does not tell the caller to call again with confirmed=true. The
// instruction is the transport's: MCP adds "approval", the CLI says --yes
// (docs/internal/cli-mcp-parity/spec.md, REQ-5; analysis.md PAR-15). Every
// tool that asks for consent (Mutating, and the two that reveal a secret) is
// driven to its preview, in English and in Russian; a tool not driven there
// fails the test, so a new gated tool needs its case here.
func TestContract_PreviewMessageHasNoConfirmedHint(t *testing.T) {
	prev := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(prev) })

	rt := &contractRT{kind: "200"}
	reg := contractReg(t, rt)
	stackHome(t)
	planned := planJob(t, `{"name":"s","vpc":{"name":"v"}}`)
	built := planJob(t, `{"name":"b","vpc":{"name":"v"}}`)
	built.Steps[0].ResourceID = contractID
	if err := built.Save(); err != nil {
		t.Fatal(err)
	}

	var gated []Tool
	for _, tl := range reg.Tools() {
		if tl.Mutating || IsSensitive(tl) {
			gated = append(gated, tl)
		}
	}
	reached := map[string]map[string]int{i18n.EN: {}, i18n.RU: {}}
	check := func(lang, name, text string) {
		t.Helper()
		var m map[string]any
		if json.Unmarshal([]byte(text), &m) != nil || m["status"] != "confirmation_required" {
			return
		}
		reached[lang][name]++
		msg, _ := m["message"].(string)
		if msg == "" {
			t.Errorf("%s %s: a preview without a message: %.200s", lang, name, text)
		}
		if strings.Contains(strings.ToLower(msg), "confirmed") {
			t.Errorf("%s %s: the preview message tells to call again with confirmed: %q", lang, name, msg)
		}
	}
	last := map[string]string{}
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		for _, tl := range gated {
			if gatedPreviewFixtures[tl.Name] != nil {
				continue
			}
			args := contractArgs(tl)
			if fix := gatedPreviewArgs[tl.Name]; fix != nil {
				fix(args)
			}
			rt.body = ""
			switch tl.Name {
			case "snat_delete", "lb_create":
				rt.body = gatedPreviewBody
			case "stack_apply":
				// Its preview refuses a job that is not there (D35).
				args["job_id"] = planned.ID
			case "stack_destroy":
				// Both previews: a stack that holds a resource, and one
				// that has created nothing.
				args["job_id"] = built.ID
				text, _, _ := contractCall(tl, args, time.Second)
				check(lang, tl.Name, text)
				args["job_id"] = planned.ID
			}
			text, _, pan := contractCall(tl, args, time.Second)
			if pan != "" {
				t.Errorf("%s %s: panic", lang, tl.Name)
			}
			check(lang, tl.Name, text)
			last[lang+" "+tl.Name] = text
		}
		rt.body = ""
	}
	// The fixtures take http.DefaultTransport: after the sweep.
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		for name, run := range gatedPreviewFixtures {
			text := run(t)
			check(lang, name, text)
			last[lang+" "+name] = text
		}
	}
	for _, lang := range []string{i18n.EN, i18n.RU} {
		for _, tl := range gated {
			if reached[lang][tl.Name] == 0 {
				t.Errorf("%s %s: not driven to its preview (give it a case: gatedPreviewArgs or gatedPreviewFixtures): %.300s",
					lang, tl.Name, last[lang+" "+tl.Name])
			}
		}
		t.Logf("%s: %d of %d gated tools reached their preview", lang, len(reached[lang]), len(gated))
	}
	if len(gated) < 60 {
		t.Fatalf("gated tools: %d", len(gated))
	}
}
