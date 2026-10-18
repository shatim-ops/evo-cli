// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// boolDefaultsTrue are the boolean arguments a handler takes as true when
// they are absent, by argument name: the tools whose schema must say
// "default": true (boolPropTrue). The CLI flag of a tree command defaults
// to true from it. deploy_plan's external_ip is not here: it is true for a
// fresh plan only (with base_plan_id the base plan's value stays).
var boolDefaultsTrue = map[string][]string{
	"release_ip":        {"destroy"},
	"kube_api_internet": {"k8s_plan", "k8s_create"},
	"embed_token":       {"k8s_kubeconfig"},
	"force":             {"secret_delete"},
	"public":            {"ca_deploy"},
	"external":          {"lb_create"},
	"refresh":           {"status"},
}

// handlerTrueRE finds a boolean argument read with a default of true:
// boolArg(args, "x", true), or x := true followed by
// if v, ok := args["x"].(bool); ok.
var handlerTrueRE = regexp.MustCompile(`[bB]oolArg\(args, "(\w+)", true\)|\w+ := true\s+if v, ok := args\["(\w+)"\]\.\(bool\); ok`)

// Every argument a handler defaults to true is in boolDefaultsTrue, and its
// tools declare "default": true; no other argument does.
func TestBoolDefaultsTrue(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	files = append(files, filepath.Join("..", "mcp", "http_policy.go"))
	found := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range handlerTrueRE.FindAllStringSubmatch(string(src), -1) {
			found[m[1]+m[2]] = true
		}
	}
	var want, got []string
	for k := range boolDefaultsTrue {
		want = append(want, k)
	}
	for k := range found {
		got = append(got, k)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("handlers default to true: %v; boolDefaultsTrue has %v", got, want)
	}

	declared := map[string]bool{}
	for _, tl := range newTestServer().Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for name, v := range props {
			if m, _ := v.(map[string]any); m["default"] == true {
				declared[tl.Name+"."+name] = true
			}
		}
	}
	for arg, toolNames := range boolDefaultsTrue {
		for _, n := range toolNames {
			if !declared[n+"."+arg] {
				t.Errorf("%s.%s: the handler defaults to true, the schema lacks \"default\": true (boolPropTrue)", n, arg)
			}
			delete(declared, n+"."+arg)
		}
	}
	for k := range declared {
		t.Errorf("%s declares \"default\": true but is not in boolDefaultsTrue", k)
	}
}
