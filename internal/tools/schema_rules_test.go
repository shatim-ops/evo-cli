// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"errors"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// schemaRule is a static rule of an argument the schema declares (D36):
// a bound of a number or the format of a string.
type schemaRule struct {
	min, max any // nil for none
	format   string
}

// schemaRules are the rules that handlers checked themselves before D36
// (minIntArg, portArg, uuidArg, checkedDelete, ranges inline): each is
// in the schema now, and CheckArgs checks it before the handler for MCP
// and the CLI alike.
var schemaRules = map[string]schemaRule{
	"deploy.disk_size":                {min: 0},
	"provision.disk_size":             {min: 0},
	"billing_finops.days":             {min: 1, max: maxFinopsDays},
	"image_create.min_cpu":            {min: 0},
	"image_create.min_ram":            {min: 0},
	"image_create.min_disk":           {min: 0},
	"k8s_add_nodepool.count":          {min: 1},
	"k8s_add_nodepool.cpu":            {min: 1},
	"k8s_add_nodepool.ram_gb":         {min: 1},
	"k8s_add_nodepool.disk_gb":        {min: 10},
	"pg_create.storage_gb":            {min: 1},
	"pg_create.instances":             {min: 1},
	"pg_create.backup_retention_days": {min: 1, max: 30},
	"lb_create.port":                  {min: 1, max: 65535},
	"lb_create.target_port":           {min: 1, max: 65535},
	"lb_create.hc_port":               {min: 1, max: 65535},
	"kafka_get.cluster_id":            {format: "uuid"},
	"pg_get.id":                       {format: "uuid"},
	"pg_get.cluster_id":               {format: "uuid"},
	"redis_get.cluster_id":            {format: "uuid"},
	"vpc_get.id":                      {format: "uuid"},
	"vpc_routes.vpc_id":               {format: "uuid"},
	"destroy.vm_id":                   {format: "uuid"},
	"disk_delete.disk_id":             {format: "uuid"},
	"image_delete.image_id":           {format: "uuid"},
	"release_floating_ip.id":          {format: "uuid"},
	"snat_delete.id":                  {format: "uuid"},
}

// The rules a handler checked itself are in the schema (D36).
func TestArgs_SchemaRulesDeclared(t *testing.T) {
	r := New(nil)
	for key, want := range schemaRules {
		tool, arg, _ := strings.Cut(key, ".")
		tl, ok := r.Lookup(tool)
		if !ok {
			t.Errorf("%s: no tool %s", key, tool)
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		p, _ := props[arg].(map[string]any)
		if p == nil {
			t.Errorf("%s: %s takes no %s", key, tool, arg)
			continue
		}
		if want.min != nil && p["minimum"] != want.min || want.max != nil && p["maximum"] != want.max {
			t.Errorf("%s: minimum %v maximum %v, want %v %v", key, p["minimum"], p["maximum"], want.min, want.max)
		}
		if p["format"] != nil || want.format != "" {
			if f, _ := p["format"].(string); f != want.format {
				t.Errorf("%s: format %q, want %q", key, f, want.format)
			}
		}
	}
}

// CheckArgs refuses a value out of the schema's bounds or of another
// format than the schema's, as USAGE naming the argument, before any
// handler; a value at the bound passes (D36). Every rule of every tool,
// not only those above.
func TestArgs_SchemaConstraintsEnforced(t *testing.T) {
	good := map[string]any{"uuid": "00000000-0000-4000-8000-000000000001", "cidr": "10.0.0.0/16"}
	bad := map[string][]any{"uuid": {"bad", "vm-1", "00000000-0000-4000-8000-00000000000g"}, "cidr": {"bad", "10.0.0.0", "10.0.0.0/33"}}
	refused := func(t *testing.T, tl Tool, arg string, v any) {
		t.Helper()
		err := CheckArgs(tl, map[string]any{arg: v})
		var ce *clierrors.CLIError
		if !errors.As(err, &ce) || ce.Code != clierrors.CodeUsage || !strings.Contains(ce.Message, arg) {
			t.Errorf("%s %s=%v: %v, want USAGE naming %s", tl.Name, arg, v, err, arg)
		}
	}
	passed := func(t *testing.T, tl Tool, arg string, v any) {
		t.Helper()
		if err := CheckArgs(tl, map[string]any{arg: v}); err != nil {
			t.Errorf("%s %s=%v at the bound: %v", tl.Name, arg, v, err)
		}
	}
	rules := 0
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for arg, pv := range props {
			p, _ := pv.(map[string]any)
			if n, ok := p["minimum"].(int); ok {
				rules++
				refused(t, tl, arg, float64(n-1))
				passed(t, tl, arg, float64(n))
			}
			if n, ok := p["maximum"].(int); ok {
				rules++
				refused(t, tl, arg, float64(n+1))
				passed(t, tl, arg, float64(n))
			}
			if f, ok := p["format"].(string); ok && good[f] != nil {
				rules++
				for _, v := range bad[f] {
					refused(t, tl, arg, v)
				}
				passed(t, tl, arg, good[f])
			}
		}
	}
	if rules < len(schemaRules) {
		t.Errorf("%d rules in the schemas, fewer than the %d declared", rules, len(schemaRules))
	}
}
