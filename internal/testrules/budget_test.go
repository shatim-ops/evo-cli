// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// budgetReach are the MCP tools each wait of production code that asks the
// cloud (a line of sleepWaits with a test) is reached from: the tools D69's
// limit holds (mcpWaits in internal/tools/mcp_budget_test.go). A wait no
// tool reaches says so with an empty list.
var budgetReach = map[string][]string{
	"internal/engine/deploy.go\tEngine.provisionVM":             {"deploy"},
	"internal/engine/deploy.go\tEngine.awaitCreated":            {"deploy"},
	"internal/engine/deploy.go\tEngine.waitBootFIP":             {"deploy", "provision", "stack_apply"},
	"internal/engine/deploy.go\tEngine.settledBootFIP":          {"deploy", "provision", "stack_apply"},
	"internal/engine/destroy.go\tEngine.Destroy":                {"destroy", "stack_destroy"},
	"internal/engine/destroy.go\tEngine.releaseFloatingIP":      {"destroy", "stack_destroy"},
	"internal/engine/destroy.go\tEngine.waitAndDeleteVM":        {"destroy", "stack_destroy"},
	"internal/tools/tools.go\tRegistry.provisionVM":             {"provision", "stack_apply"},
	"internal/tools/opwait.go\twaitForOpJSON":                   {"ar_operation", "cert_operation", "dns_operation", "k8s_nodepool_scale", "k8s_operation", "kafka_operation", "lb_operation", "pg_operation", "redis_operation", "vpc_operation", "dns_zone_delete"},
	"internal/tools/opwait.go\twaitState":                       {"disk_create", "disk_attach", "disk_detach", "disk_resize", "vm_start", "vm_stop", "vm_reboot", "k8s_nodepool_scale"},
	"internal/tools/tools_stack.go\tRegistry.handleStackStatus": {"stack_status"},
	"internal/engine/deploy.go\tassignFloatingIP":               {"deploy", "provision", "stack_apply"},
	"internal/engine/deploy.go\tEngine.findBootFIP":             {"deploy", "provision", "stack_apply"},
	"internal/engine/deploy.go\tEngine.releaseBootFIP":          {"deploy", "provision", "stack_apply"},
	"internal/engine/deploy.go\tEngine.waitVMGone":              {"deploy", "destroy", "stack_destroy"},
	"internal/deployplan/apply.go\tfinishApply":                 {"deploy_apply"},
	// The server's loop: it serves the calls, no tool waits in it.
	"internal/mcp/server.go\tServer.RunContext": {},
}

// Every wait that asks the cloud is held to D69's limit through the tools
// that reach it (process plan 1.7 §1, part 1: coverage from the waits, not
// from a list kept by hand): a wait of sleepWaits with a test names its
// tools in budgetReach, and each of them is a line of mcpWaits. A new wait
// fails TestWaits_SleepThroughIsTested until it is in sleepWaits, then this
// one until its tools are held to the limit.
func TestWaits_ReachedToolsHoldTheBudget(t *testing.T) {
	waits := mcpWaitsOf(t, filepath.Join("..", "tools", "mcp_budget_test.go"))
	var bad []string
	for k, w := range sleepWaits {
		tools, ok := budgetReach[k]
		switch {
		case w.test == "" && ok:
			bad = append(bad, k+": in budgetReach, but sleepWaits says it asks no cloud ("+w.why+")")
		case w.test != "" && !ok:
			bad = append(bad, k+": a wait that asks the cloud, not in budgetReach; name the MCP tools that reach it")
		}
		for _, tl := range tools {
			if !waits[tl] {
				bad = append(bad, k+": reached from "+tl+", which is not in mcpWaits (internal/tools/mcp_budget_test.go)")
			}
		}
	}
	for k := range budgetReach {
		if _, ok := sleepWaits[k]; !ok {
			bad = append(bad, k+": in budgetReach, not in sleepWaits")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

// mcpWaitsOf reads the keys of the map literal mcpWaits in path.
func mcpWaitsOf(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "mcpWaits" || len(vs.Values) != 1 {
			return true
		}
		lit, _ := vs.Values[0].(*ast.CompositeLit)
		if lit == nil {
			return false
		}
		for _, e := range lit.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				if b, ok := kv.Key.(*ast.BasicLit); ok && b.Kind == token.STRING {
					if s, err := strconv.Unquote(b.Value); err == nil {
						keys[s] = true
					}
				}
			}
		}
		return false
	})
	if len(keys) < 10 {
		t.Fatalf("%s: %d keys of mcpWaits found: the scan sees nothing", path, len(keys))
	}
	return keys
}
