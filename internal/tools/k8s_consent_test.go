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

	"github.com/shatim-ops/evo-cli/internal/mk8s"
)

// D29 (delta audit 1.2.3, FB123-4…7): consent is asked for a create that
// can go through, and names what it creates.

// FB123-5: the preview of k8s_create resolves the plan — the masters'
// flavor, the version, the zone, the VIP subnet — so the person consents to
// the cluster that will be made; a plan that cannot be resolved is refused
// before the question, not after the yes.
func TestK8sCreate_PreviewResolvesThePlan(t *testing.T) {
	f := &raAPI{flavors: `[
		{"flavorId":"fl-4-8","name":"gen-4-8","cpu":4,"ram":8},
		{"flavorId":"fl-2-4","name":"gen-2-4","cpu":2,"ram":4}]`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	m, text, _ := callNamed(t, r, "k8s_create", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1"})
	if m["status"] != "confirmation_required" {
		t.Fatalf("preview: %s", text)
	}
	preview, _ := json.Marshal(m["preview"])
	for _, want := range []string{"gen-2-4", "v1.34.6", "ru.AZ-1", "sn-1"} {
		if !strings.Contains(string(preview), want) {
			t.Errorf("the preview does not name %s: %s", want, preview)
		}
	}

	for _, c := range []struct {
		label  string
		args   map[string]any
		reason string
	}{
		{"subnet of another zone", map[string]any{"zone": "ru.AZ-1", "subnet_id": "sn-2"}, "CONFIG_INVALID"},
		{"unknown zone", map[string]any{"zone": "ru.AZ-9"}, "USAGE"},
		{"unknown flavor", map[string]any{"zone": "ru.AZ-1", "flavor": "gen-9-9"}, "USAGE"},
	} {
		args := map[string]any{"cluster_sa_id": "sa-1"}
		for k, v := range c.args {
			args[k] = v
		}
		m, text, isErr := callNamed(t, r, "k8s_create", args)
		if !isErr || m["status"] == "confirmation_required" {
			t.Errorf("%s: asked for consent: %s", c.label, text)
			continue
		}
		wantReason(t, c.label, m, text, c.reason)
	}
	if n := f.postCount(); n != 0 {
		t.Errorf("previews sent %d POST", n)
	}
}

// FB123-7: an explicit flavor that has a GPU, or that is not in the
// catalogue, gets an error of its own, not "no matching flavor (cpu>=2
// ram>=4)" for cpu and ram nobody asked; for the masters and the nodes
// alike, before the consent.
func TestK8s_ExplicitFlavorRefusedForWhatItIs(t *testing.T) {
	f := &raAPI{flavors: `[
		{"flavorId":"fl-gpu","name":"gpu-2-4","cpu":2,"ram":4,"gpu":1},
		{"flavorId":"fl-2-4","name":"gen-2-4","cpu":2,"ram":4}]`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	for _, c := range []struct{ flavor, want string }{{"gpu-2-4", "GPU"}, {"gen-9-9", "not in the catalogue"}} {
		for _, call := range []struct {
			tool string
			args map[string]any
		}{
			{"k8s_plan", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1"}},
			{"k8s_create", map[string]any{"zone": "ru.AZ-1", "cluster_sa_id": "sa-1"}},
			{"k8s_add_nodepool", map[string]any{"cluster_id": "cl-1", "security_group_id": "sg-1"}},
		} {
			call.args["flavor"] = c.flavor
			m, text, isErr := callNamed(t, r, call.tool, call.args)
			msg, _ := m["error"].(string)
			if !isErr || !strings.Contains(msg, c.want) || strings.Contains(msg, "cpu>=") {
				t.Errorf("%s --flavor %s: %s", call.tool, c.flavor, text)
				continue
			}
			wantReason(t, call.tool+" "+c.flavor, m, text, "USAGE")
		}
	}
	if n := f.postCount(); n != 0 {
		t.Errorf("refusals sent %d POST", n)
	}
}

// FB123-6: one choice of a k8s flavor for the masters and the nodes — the
// smallest without a GPU that fits, ties by name and id whatever the order
// of the catalogue (D27), an explicit name taken as it is.
func TestPickK8sFlavor(t *testing.T) {
	fl := func(id, name string, cpu, ram, gpu int) mk8s.Flavor {
		return mk8s.Flavor{FlavorID: id, Name: name, CPU: cpu, RAM: ram, GPU: gpu}
	}
	catalogue := []mk8s.Flavor{
		fl("fl-16", "gen-16-128", 16, 128, 0), fl("fl-gpu", "gpu-2-4", 2, 4, 1), fl("fl-b", "gen-2-4-b", 2, 4, 0),
		fl("fl-2-2", "gen-2-2", 2, 2, 0), fl("fl-a", "gen-2-4-a", 2, 4, 0), fl("fl-4", "gen-4-8", 4, 8, 0),
	}
	reversed := make([]mk8s.Flavor, len(catalogue))
	for i, f := range catalogue {
		reversed[len(catalogue)-1-i] = f
	}
	for _, flavors := range [][]mk8s.Flavor{catalogue, reversed} {
		for _, c := range []struct {
			want       string
			cpu, ram   int
			wantID     string
			wantErrSub string
		}{
			{"", 2, 4, "fl-a", ""},
			{"", 3, 4, "fl-4", ""},
			{"gen-16-128", 2, 4, "fl-16", ""},
			{"gpu-2-4", 2, 4, "", "GPU"},
			{"gen-9-9", 2, 4, "", "not in the catalogue"},
			{"", 32, 256, "", "cpu>=32 ram>=256"},
		} {
			got, err := pickK8sFlavor(flavors, c.want, c.cpu, c.ram)
			switch {
			case c.wantErrSub != "" && (err == nil || !strings.Contains(err.Error(), c.wantErrSub)):
				t.Errorf("%q cpu %d ram %d: err %v, want %q", c.want, c.cpu, c.ram, err, c.wantErrSub)
			case c.wantErrSub == "" && (err != nil || got.FlavorID != c.wantID):
				t.Errorf("%q cpu %d ram %d: %v %v, want %s", c.want, c.cpu, c.ram, got.FlavorID, err, c.wantID)
			}
		}
	}
}

// FB123-6: the nodes' flavor went by the order of the catalogue among equal
// ones (the copy had no tie-break of D27); now the same choice as the
// masters'.
func TestK8sAddNodePool_EqualFlavorsByName(t *testing.T) {
	for _, flavors := range []string{
		`[{"flavorId":"fl-b","name":"gen-2-4-b","cpu":2,"ram":4},{"flavorId":"fl-a","name":"gen-2-4-a","cpu":2,"ram":4}]`,
		`[{"flavorId":"fl-a","name":"gen-2-4-a","cpu":2,"ram":4},{"flavorId":"fl-b","name":"gen-2-4-b","cpu":2,"ram":4}]`,
	} {
		f := &raAPI{flavors: flavors}
		r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
		if _, text, isErr := callNamed(t, r, "k8s_add_nodepool", map[string]any{"cluster_id": "cl-1", "security_group_id": "sg-1", "confirmed": true}); isErr {
			t.Fatalf("k8s_add_nodepool: %s", text)
		}
		mc, _ := f.post("/v3/nodePools")["machineConfiguration"].(map[string]any)
		if mc["flavorId"] != "fl-a" {
			t.Errorf("catalogue %s: flavor %v, want fl-a", flavors, mc["flavorId"])
		}
	}
}

// FB123-4: ar_create refuses a taken name before the consent, as the other
// creates (D20, D21): the question was asked for a call that could not go
// through.
func TestArCreate_TakenNameBeforeConsent(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "ar.api.cloud.ru" {
			return false
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/registries" {
			_, _ = io.WriteString(w, `{"registries":[{"id":"`+auditUUID+`","name":"reg","status":"ACTIVE"}]}`)
			return true
		}
		_, _ = io.WriteString(w, `{"id":"op-1","done":false}`)
		return true
	}}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "ar_create", map[string]any{"name": "reg"})
	if !isErr || m["reason"] != "USAGE" || m["existing_id"] != auditUUID {
		t.Errorf("taken name, no consent yet: %s", text)
	}
	if m, text, _ = callNamed(t, r, "ar_create", map[string]any{"name": "fresh"}); m["status"] != "confirmation_required" {
		t.Errorf("free name: %s", text)
	}
	if _, ok := f.posted("ar.api.cloud.ru"); ok {
		t.Error("a preview created a registry")
	}
}
