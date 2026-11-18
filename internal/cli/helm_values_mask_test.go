// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

// A helm that fails quoting what it was given — its arguments and the
// values file — does not carry into evo's answer a value evo passed under a
// key of a secret (set, values) or the password of an address under any
// key: over MCP (stdio, HTTP) and in the CLI, human and --json. What is no
// secret stays, and so does helm's error (#2465; «Риск» to the issue:
// variant Б, the userinfo of an address under any key).
func TestHelm_SecretValuesNotEchoedInTheAnswer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the world's helm is a shell script")
	}
	secrets := []string{"HELMSET2465PW", "HELMURL2465PW", "HELMVAL2465PW", "HELMVALURL2465PW"}
	kept := []string{"web-visible-2465", "tag-1.27", "Error: render failed", "@db:5432/x", "@h/db"}
	w, _ := withJSONWorld(t)
	srv := parityServers(t)
	putFakeKube(t, `echo "Error: render failed: $*"; for a in "$@"; do [ -f "$a" ] && cat "$a"; done; exit 1`)
	args := map[string]any{"cluster_id": addonClusterID, "action": "install", "release": "web", "chart": "repo/web",
		"set":       []any{"db.password=" + secrets[0], "externalDb.url=postgres://app:" + secrets[1] + "@db:5432/x", "image.tag=tag-1.27"},
		"values":    "auth:\n  rootPassword: " + secrets[2] + "\nexternal:\n  dsn: mysql://u:" + secrets[3] + "@h/db\nservice:\n  name: web-visible-2465\n",
		"confirmed": true}
	check := func(where, text string) {
		t.Helper()
		for _, s := range secrets {
			if strings.Contains(text, s) {
				t.Errorf("%s: the value %s evo passed to helm is in the answer: %.600s", where, s, text)
			}
		}
		for _, k := range kept {
			if !strings.Contains(text, k) {
				t.Errorf("%s: %q, no secret, is lost: %.600s", where, k, text)
			}
		}
	}
	check("mcp stdio", string(stdioRaw(t, srv.stdio, "k8s_helm", args)))
	forgetRun(w)
	h, _ := json.Marshal(httpDetails(t, srv.http, "k8s_helm", args))
	check("mcp http", string(h))
	delete(args, "confirmed")
	cli := []string{"k8s", "helm", addonClusterID, "install", "--release", "web", "--chart", "repo/web",
		"--set", args["set"].([]any)[0].(string), "--set", args["set"].([]any)[1].(string), "--set", args["set"].([]any)[2].(string),
		"--values", args["values"].(string), "--yes"}
	for _, extra := range [][]string{nil, {"--json"}} {
		forgetRun(w)
		_, out, errOut := runCLI(t, "", append(cli, extra...)...)
		check("cli "+strings.Join(extra, " "), out+errOut)
	}
}
