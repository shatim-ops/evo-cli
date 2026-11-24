// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

func TestVPCSGAliasHelp(t *testing.T) {
	setupCLI(t)
	defer i18n.SetLang(i18n.Lang())
	for _, lang := range []string{i18n.RU, i18n.EN} {
		t.Setenv("EVO_LANG", lang)
		i18n.SetLang(lang)
		code, canonical, stderr := runCLI(t, "", "vpc", "security-groups", "--help")
		status, alias, errOut := runCLI(t, "", "vpc", "sg", "--help")
		if code != 0 || status != 0 || alias != canonical || errOut != stderr || !strings.Contains(alias, "list_security_groups") || !strings.Contains(alias, "--rules") || hasCyrillic(alias) != (lang == i18n.RU) {
			t.Fatalf("%s: canonical %d, alias %d, leaf help matches: %v", lang, code, status, canonical == alias)
		}
	}
}

func TestVPCSGAliasMatchesCLIAndMCP(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", allowedProject)
	old := httpretry.DefaultBase
	sg := &sgRulesTrip{base: old}
	httpretry.DefaultBase = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.URL.Path, "/api/v1/security-groups") && req.Method != http.MethodGet {
			t.Errorf("unexpected mutation: %s %s", req.Method, req.URL.Path)
		}
		return sg.RoundTrip(req)
	})
	t.Cleanup(func() { httpretry.DefaultBase = old })
	defer i18n.SetLang(i18n.Lang())
	for _, lang := range []string{i18n.RU, i18n.EN} {
		t.Setenv("EVO_LANG", lang)
		i18n.SetLang(lang)
		code, body, stderr := runCLI(t, "", "vpc", "security-groups", "--name", "web")
		status, out, errOut := runCLI(t, "", "vpc", "sg", "--name", "web")
		if code != 0 || code != status || out != body || errOut != stderr || !strings.Contains(out, "web") || hasCyrillic(out) != (lang == i18n.RU) {
			t.Fatal(lang, code, status, body, out, stderr, errOut)
		}
	}
	for _, args := range [][]string{{"--name", "web"}, {"--name", "absent"}, {"--rules"}, {"unexpected"}, {"--unknown-option"}} {
		canonical := append([]string{"vpc", "security-groups", "--json"}, args...)
		alias := append([]string{"vpc", "sg", "--json"}, args...)
		want, body, stderr := runCLI(t, "", canonical...)
		code, out, errOut := runCLI(t, "", alias...)
		if code != want || out != body || requestIDNamed.ReplaceAllString(errOut, "") != requestIDNamed.ReplaceAllString(stderr, "") {
			t.Fatalf("%v: canonical %d %s %s; alias %d %s %s", args, want, body, stderr, code, out, errOut)
		}
		switch args[0] {
		case "--name":
			if code != 0 || !json.Valid([]byte(out)) {
				t.Fatal(args, code, out, errOut)
			}
			var cli map[string]any
			if err := json.Unmarshal([]byte(out), &cli); err != nil {
				t.Fatal(err)
			}
			rows, _ := cli["security_groups"].([]any)
			if args[1] == "web" {
				if len(rows) != 1 || cli["count"] != float64(1) {
					t.Fatal(cli)
				}
				group, _ := rows[0].(map[string]any)
				if group["name"] != sgHostileName {
					t.Fatal(rows)
				}
			} else if len(rows) != 0 || cli["count"] != float64(0) {
				t.Fatal(cli)
			}
			for transport, call := range mcpTransports(t, nil) {
				a := call("list_security_groups", map[string]any{"name": args[1]})
				var result map[string]any
				if a.isErr || json.Unmarshal([]byte(a.text), &result) != nil || !reflect.DeepEqual(cli, result) {
					t.Fatal(transport, args, cli, a)
				}
			}
		case "--rules":
			if code != 5 || out != "" || !strings.Contains(stderr, "API_ERROR") || !strings.Contains(stderr, "bad request") {
				t.Fatal(args, code, out, errOut)
			}
			for transport, call := range mcpTransports(t, nil) {
				a := call("list_security_groups", map[string]any{"rules": true})
				if !a.isErr || !strings.Contains(a.text, "bad request") {
					t.Fatal(transport, a)
				}
			}
		default:
			if code != 1 || !strings.Contains(stderr, "USAGE") || out != "" {
				t.Fatal(args, code, out, errOut)
			}
		}
	}
}
